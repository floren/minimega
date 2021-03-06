package main

import (
	"errors"
	"fmt"
	"math/rand"
	log "minilog"
	"net"
	"strconv"
	"strings"
	"time"
)

// Returns the node numbers within the given array of nodes of all contiguous sets of '0' entries
func findContiguousBlock(nodes []uint64, count int) ([][]int, error) {
	result := [][]int{}
	for i := 0; i+count <= len(nodes); i++ {
		if isFree(nodes, i, count) {
			inner := []int{}
			for j := 0; j < count; j++ {
				inner = append(inner, i+j)
			}
			result = append(result, inner)
		}
	}
	if len(result) > 0 {
		return result, nil
	} else {
		return result, errors.New("no space available in this slice")
	}
}

func areNodesFree(clusternodes []uint64, requestedindexes []int) bool {
	for _, idx := range requestedindexes {
		if !isFree(clusternodes, idx, 1) {
			return false
		}
	}
	return true
}

// Returns true if nodes[index] through nodes[index+count-1] are free
func isFree(nodes []uint64, index, count int) bool {
	for i := index; i < index+count; i++ {
		if nodes[index] != 0 {
			return false
		}
	}
	return true
}

func findReservation(minutes, nodecount int) (Reservation, []TimeSlice, error) {
	return findReservationAfter(minutes, nodecount, time.Now().Unix())
}

// Finds a slice of 'nodecount' nodes that's available for the specified length of time
// Returns a reservation and a slice of TimeSlices that can be used to replace
// the current Schedule if the reservation is acceptable.
// The 'after' parameter specifies a Unix timestamp that should be taken as the
// starting time for our search (this allows you to say "give me the first reservation
// after noon tomorrow")
func findReservationAfter(minutes, nodecount int, after int64) (Reservation, []TimeSlice, error) {
	return findReservationGeneric(minutes, nodecount, []string{}, false, after)
}

// Finds a slice of 'nodecount' nodes that's available for the specified length of time
// Returns a reservation and a slice of TimeSlices that can be used to replace
// the current Schedule if the reservation is acceptable.
// The 'after' parameter specifies a Unix timestamp that should be taken as the
// starting time for our search (this allows you to say "give me the first reservation
// after noon tomorrow")
// 'requestednodes' = a list of node names
// 'specific' = true if we want the nodes in requestednodes rather than a range
func findReservationGeneric(minutes, nodecount int, requestednodes []string, specific bool, after int64) (Reservation, []TimeSlice, error) {
	var res Reservation
	var err error
	var newSched []TimeSlice

	slices := minutes / MINUTES_PER_SLICE
	if (minutes % MINUTES_PER_SLICE) != 0 {
		slices++
	}

	// convert hostnames to indexes
	var requestedindexes []int
	for _, hostname := range requestednodes {
		ns := strings.TrimPrefix(hostname, igorConfig.Prefix)
		n, err := strconv.Atoi(ns)
		if err != nil {
			return res, newSched, errors.New("invalid hostname " + hostname)
		}
		requestedindexes = append(requestedindexes, n-igorConfig.Start)
	}

	res.ID = uint64(rand.Int63())

	// We start with the *second* time slice, because the first is the current slice
	// and is partially consumed
	for i := 1; ; i++ {
		// Make sure the Schedule has enough time left in it
		if len(Schedule[i:])*MINUTES_PER_SLICE <= minutes {
			// This will guarantee we'll have enough space for the reservation
			extendSchedule(minutes)
		}

		if Schedule[i].Start < after {
			continue
		}

		s := Schedule[i]
		var blocks [][]int
		// Check if there's any open blocks in this slice
		if specific {
			if areNodesFree(s.Nodes, requestedindexes) {
				blocks = [][]int{requestedindexes}
			} else {
				continue
			}
		} else {
			blocks, err = findContiguousBlock(s.Nodes, nodecount)
			if err != nil {
				continue
			}
		}

		// For each of the blocks...
		for _, b := range blocks {
			// Make a new starter schedule
			newSched = Schedule
			var nodenames []string
			for j := 0; j < slices; j++ {
				nodenames = []string{}
				// For simplicity, we'll end up re-checking the first slice, but who cares
				if !areNodesFree(newSched[i+j].Nodes, b) {
					break
				} else {
					// Mark those nodes reserved
					//for k := b; k < b+nodecount; k++ {
					for _, idx := range b {
						newSched[i+j].Nodes[idx] = res.ID
						fmtstring := "%s%0" + strconv.Itoa(igorConfig.Padlen) + "d"
						nodenames = append(nodenames, fmt.Sprintf(fmtstring, igorConfig.Prefix, igorConfig.Start+idx))
					}
				}
			}
			// If we got this far, that means this block was free long enough
			// Now just fill out the rest of the reservation and we're all set
			var IPs []net.IP
			// First, go from node name to PXE filename
			for _, hostname := range nodenames {
				ip, err := net.LookupIP(hostname)
				if err != nil {
					log.Fatal("failure looking up %v: %v", hostname, err)
				}
				IPs = append(IPs, ip...)
			}
			// Now go IP->hex
			for _, ip := range IPs {
				res.PXENames = append(res.PXENames, toPXE(ip))
			}
			res.Hosts = nodenames
			res.StartTime = newSched[i].Start
			res.EndTime = res.StartTime + int64(minutes*60)
			res.Duration = time.Unix(res.EndTime, 0).Sub(time.Unix(res.StartTime, 0)).Minutes()
			goto Done
		}
	}
Done:
	return res, newSched, nil
}

func initializeSchedule() {
	// Create a 'starter'
	start := time.Now().Truncate(time.Minute * MINUTES_PER_SLICE) // round down
	end := start.Add((MINUTES_PER_SLICE-1)*time.Minute + 59*time.Second)
	size := igorConfig.End - igorConfig.Start + 1
	ts := TimeSlice{Start: start.Unix(), End: end.Unix()}
	ts.Nodes = make([]uint64, size)
	Schedule = []TimeSlice{ts}

	// Now expand it to fit the minimum size we want
	extendSchedule(MIN_SCHED_LEN - MINUTES_PER_SLICE) // we already have one slice so subtract
}

func expireSchedule() {
	// If the last element of the schedule is expired, or it's empty, let's start fresh
	if len(Schedule) == 0 || Schedule[len(Schedule)-1].End < time.Now().Unix() {
		initializeSchedule()
	}

	// Get rid of any outdated TimeSlices
	for i, t := range Schedule {
		if t.End > time.Now().Unix() {
			Schedule = Schedule[i:]
			break
		}
	}

	// Now make sure we have at least the minimum length there
	if len(Schedule)*MINUTES_PER_SLICE < MIN_SCHED_LEN {
		// Expand that schedule
		extendSchedule(MIN_SCHED_LEN - len(Schedule)*MINUTES_PER_SLICE)
	}
}

func extendSchedule(minutes int) {
	size := igorConfig.End - igorConfig.Start + 1 // size of node slice

	slices := minutes / MINUTES_PER_SLICE
	if (minutes % MINUTES_PER_SLICE) != 0 {
		// round up
		slices++
	}
	prev := Schedule[len(Schedule)-1]
	for i := 0; i < slices; i++ {
		// Everything's in Unix time, which is in units of seconds
		start := prev.End + 1 // Starts 1 second after the previous reservation ends
		end := start + (MINUTES_PER_SLICE-1)*60 + 59
		ts := TimeSlice{Start: start, End: end}
		ts.Nodes = make([]uint64, size)
		Schedule = append(Schedule, ts)
		prev = ts
	}
}
