package commander

import (
	"fmt"
	"io"
	"os/exec"
)

type Cmd struct {
	cmd    *exec.Cmd
	Stdout io.ReadCloser
	Stdin  io.WriteCloser
	Stderr io.ReadCloser
}

func (c *Cmd) String() string {
	return fmt.Sprintf("%v", c.cmd)
}

func New(path string, args []string) (*Cmd, error) {
	var err error

	ret := &Cmd{}
	c := &exec.Cmd{
		Path: path,
		Args: args,
		Env:  nil,
		Dir:  "",
	}

	ret.Stdout, err = c.StdoutPipe()
	if err != nil {
		return nil, err
	}

	ret.Stdin, err = c.StdinPipe()
	if err != nil {
		return nil, err
	}

	ret.Stderr, err = c.StderrPipe()
	if err != nil {
		return nil, err
	}

	ret.cmd = c

	return ret, nil
}

func (c *Cmd) Run() error {
	return c.cmd.Run()
}
