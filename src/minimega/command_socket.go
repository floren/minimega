// Copyright (2012) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"encoding/json"
	"io"
	"minicli"
	"miniclient"
	log "minilog"
	"net"
	"os"
)

func commandSocketStart() {
	l, err := net.Listen("unix", *f_base+"minimega")
	if err != nil {
		log.Error("commandSocketStart: %v", err)
		teardown()
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Error("commandSocketStart: accept: %v", err)
		}
		log.Infoln("client connected")

		go commandSocketHandle(conn)
	}
}

func commandSocketRemove() {
	f := *f_base + "minimega"
	err := os.Remove(f)
	if err != nil {
		log.Errorln(err)
	}
}

func commandSocketHandle(c net.Conn) {
	var err error

	enc := json.NewEncoder(c)
	dec := json.NewDecoder(c)

outer:
	for err == nil {
		var cmd *minicli.Command
		cmd, err = readLocalCommand(dec)
		if err != nil {
			if err != io.EOF {
				// Must be incompatible versions of minimega... F***
				log.Errorln(err)
			}
			break
		}
		err = nil

		var prevResp minicli.Responses

		if cmd != nil {
			// HAX: Don't record the read command
			if hasCommand(cmd, "read") {
				cmd.Record = false
			}

			// HAX: Work around so that we can add the more boolean
			for resp := range runCommand(cmd) {
				if prevResp != nil {
					err = sendLocalResp(enc, prevResp, true)
					if err != nil {
						break outer
					}
				}

				prevResp = resp
			}
		}
		if err == nil {
			err = sendLocalResp(enc, prevResp, false)
		}
	}

	if err != nil {
		if err == io.EOF {
			log.Infoln("command client disconnected")
		} else {
			log.Errorln(err)
		}
	}
}

func readLocalCommand(dec *json.Decoder) (*minicli.Command, error) {
	var cmd minicli.Command
	err := dec.Decode(&cmd)
	if err != nil {
		return nil, err
	}

	log.Debug("got command over socket: %v", cmd)

	// HAX: Reprocess the original command since the Call target cannot be
	// serialized... is there a cleaner way to do this?
	return minicli.Compile(cmd.Original)
}

func sendLocalResp(enc *json.Encoder, resp minicli.Responses, more bool) error {
	log.Info("sending resp: %v", resp)
	r := miniclient.Response{
		More: more,
	}
	if resp != nil {
		r.Resp = resp
		r.Rendered = resp.String()
	}

	return enc.Encode(&r)
}
