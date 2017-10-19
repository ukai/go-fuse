// Copyright 2016 the Go-FUSE Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fuse

import (
	"os"

	"github.com/hanwen/go-fuse/splice"
)

func (s *Server) setSplice() {
	s.canSplice = splice.Resizable()
}

// trySplice:  Zero-copy read from fdData.Fd into /dev/fuse
//
// 1) Write header (assuming fdData.Size() bytes of data) into the "pair" pipe buffer               --> pair2: [header]
// 2) Splice data from "fdData" into "pair"; check number of bytes. If less than expected: bail, falling back to standard write.
//
func (ms *Server) trySplice(header []byte, req *request, fdData *readResultFd) error {
	fdSize := fdData.Size()

	// Get a pair of connected pipes
	pair, err := splice.Get()
	if err != nil {
		return err
	}
	defer splice.Done(pair)

	// Grow buffer pipe to requested size + one extra page
	// Without the extra page the kernel will block once the pipe is almost full
	if err := pair.Grow(fdSize + os.Getpagesize()); err != nil {
		return err
	}
	header = req.serializeHeader(fdSize)
	if _, err := pair.Write(header); err != nil {
		// TODO - extract the data from splice?
		return err
	}

	// Read data from file
	if n, err := pair.LoadFromAt(fdData.Fd, fdSize, fdData.Off); err != nil {
		return err
	} else if n != fdData.Size() {
		// We get a short read at end of file: fall back to
		// normal read.  We could avoid reading the data into
		// user space (read to discard header, write new
		// header, splice data), but at 3 system calls, that
		// may be slower overall? This is certainly simpler.
		return errSpliceShortRead
	}

	// Write header + data to /dev/fuse
	_, err = pair.WriteTo(uintptr(ms.mountFd), fdSize+int(sizeOfOutHeader))
	if err != nil {
		return err
	}

	return nil
}
