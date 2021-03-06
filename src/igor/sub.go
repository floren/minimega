// Copyright (2013) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"fmt"
	"io"
	log "minilog"
	"os"
	"os/user"
	"path/filepath"
	"ranges"
	"time"
)

var cmdSub = &Command{
	UsageLine: "sub -r <reservation name> -k <kernel path> -i <initrd path> {-n <integer> | -w <node list>} [OPTIONS]",
	Short:     "create a reservation",
	Long: `
Create a new reservation.

REQUIRED FLAGS:

The -r flag sets the name for the reservation.

The -k flag gives the location of the kernel the nodes should boot. This
kernel will be copied to a separate directory for use.

The -i flag gives the location of the initrd the nodes should boot. This
file will be copied to a separate directory for use.

The -n flag indicates that the specified number of nodes should be
included in the reservation. The first available nodes will be allocated.

OPTIONAL FLAGS:

The -c flag sets any kernel command line arguments. (eg "console=tty0").

The -t flag is used to specify the reservation time in integer minutes. (default = 60)

The -s flag is a boolean to enable 'speculative' mode; this will print a selection of available times for the reservation, but will not actually make the reservation. Intended to be used with the -a flag to select a specific time slot.

The -a flag indicates that the reservation should take place on or after the specified time, given in the format "Jan 2 15:04". Especially useful in conjunction with the -s flag.
	`,
}

var subR string // -r flag
var subK string // -k flag
var subI string // -i
var subN int    // -n
var subC string // -c
var subT int    // -t
var subS bool   // -s
var subA string // -a
var subW string // -w

func init() {
	// break init cycle
	cmdSub.Run = runSub

	cmdSub.Flag.StringVar(&subR, "r", "", "")
	cmdSub.Flag.StringVar(&subK, "k", "", "")
	cmdSub.Flag.StringVar(&subI, "i", "", "")
	cmdSub.Flag.IntVar(&subN, "n", 0, "")
	cmdSub.Flag.StringVar(&subC, "c", "", "")
	cmdSub.Flag.IntVar(&subT, "t", 60, "")
	cmdSub.Flag.BoolVar(&subS, "s", false, "")
	cmdSub.Flag.StringVar(&subA, "a", "", "")
	cmdSub.Flag.StringVar(&subW, "w", "", "")
}

func runSub(cmd *Command, args []string) {
	var nodes []string          // if the user has requested specific nodes
	var reservation Reservation // the new reservation
	var newSched []TimeSlice    // the new schedule
	format := "2006-Jan-2-15:04"

	// validate arguments
	if subR == "" || subK == "" || subI == "" || (subN == 0 && subW == "") {
		help([]string{"sub"})
		log.Fatalln("Missing required argument")

	}

	user, err := user.Current()
	if err != nil {
		log.Fatalln("cannot determine current user", err)
	}

	// Make sure there's not already a reservation with this name
	for _, r := range Reservations {
		if r.ResName == subR {
			log.Fatalln("A reservation named ", subR, " already exists.")
		}
	}

	// figure out which nodes to reserve
	if subW != "" {
		rnge, _ := ranges.NewRange(igorConfig.Prefix, igorConfig.Start, igorConfig.End)
		nodes, _ = rnge.SplitRange(subW)
	}

	when := time.Now()
	if subA != "" {
		loc, _ := time.LoadLocation("Local")
		t, _ := time.Parse(format, subA)
		when = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	}

	// If this is a speculative call, run findReservationAfter a few times,
	// print, and exit
	if subS {
		fmt.Println("AVAILABLE RESERVATIONS")
		fmt.Println("START\t\t\tEND")
		for i := 0; i < 10; i++ {
			var r Reservation
			if subN > 0 {
				r, _, err = findReservationAfter(subT, subN, when.Add(time.Duration(i*10)*time.Minute).Unix())
				if err != nil {
					log.Fatalln(err)
				}
			} else if subW != "" {
				r, _, err = findReservationGeneric(subT, 0, nodes, true, when.Add(time.Duration(i*10)*time.Minute).Unix())
				if err != nil {
					log.Fatalln(err)
				}
			}
			fmt.Printf("%v\t%v\n", time.Unix(r.StartTime, 0).Format(format), time.Unix(r.EndTime, 0).Format(format))
		}
		return
	}

	if subN > 0 {
		reservation, newSched, err = findReservationAfter(subT, subN, when.Unix())
	} else if subW != "" {
		reservation, newSched, err = findReservationGeneric(subT, 0, nodes, true, when.Unix())
	}
	if err != nil {
		log.Fatalln(err)
	}

	// pick a network segment
	var vlan int
	for vlan = igorConfig.VLANMin; vlan <= igorConfig.VLANMax; vlan++ {
		for _, r := range Reservations {
			if vlan == r.Vlan {
				continue
			}
		}
		break
	}
	if vlan > igorConfig.VLANMax {
		log.Fatal("couldn't assign a vlan!")
	}
	reservation.Vlan = vlan

	reservation.Owner = user.Username
	reservation.ResName = subR
	reservation.KernelArgs = subC

	// Add it to the list of reservations
	Reservations[reservation.ID] = reservation

	// copy kernel and initrd
	// 1. Validate and open source files
	ksource, err := os.Open(subK)
	if err != nil {
		log.Fatal("couldn't open kernel: %v", err)
	}
	isource, err := os.Open(subI)
	if err != nil {
		log.Fatal("couldn't open initrd: %v", err)
	}

	// make kernel copy
	fname := filepath.Join(igorConfig.TFTPRoot, "igor", subR+"-kernel")
	kdest, err := os.Create(fname)
	if err != nil {
		log.Fatal("failed to create %v -- %v", fname, err)
	}
	io.Copy(kdest, ksource)
	kdest.Close()
	ksource.Close()

	// make initrd copy
	fname = filepath.Join(igorConfig.TFTPRoot, "igor", subR+"-initrd")
	idest, err := os.Create(fname)
	if err != nil {
		log.Fatal("failed to create %v -- %v", fname, err)
	}
	io.Copy(idest, isource)
	idest.Close()
	isource.Close()

	timefmt := "Jan 2 15:04"
	rnge, _ := ranges.NewRange(igorConfig.Prefix, igorConfig.Start, igorConfig.End)
	fmt.Printf("Reservation created for %v - %v\n", time.Unix(reservation.StartTime, 0).Format(timefmt), time.Unix(reservation.EndTime, 0).Format(timefmt))
	unsplit, _ := rnge.UnsplitRange(reservation.Hosts)
	fmt.Printf("Nodes: %v\n", unsplit)

	Schedule = newSched

	// update the network config
	//err = networkSet(reservation.Hosts, vlan)
	//if err != nil {
	//	log.Fatal("error setting network isolation: %v", err)
	//}

	putReservations()
	putSchedule()
}
