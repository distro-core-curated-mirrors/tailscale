// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

func (dc *DiskCache) writeActionFile(b []byte, actionID string) error {
	dest := dc.ActionFilename(actionID)
	_, err := writeAtomic(dest, bytes.NewReader(b))
	return err
}

func (dc *DiskCache) writeOutputFile(r io.Reader, _ int64, outputID string) (int64, error) {
	dest := dc.OutputFilename(outputID)
	return writeAtomic(dest, r)
}

func writeAtomic(dest string, r io.Reader) (int64, error) {
	tf, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".*")
	if err != nil {
		return 0, err
	}
	size, err := io.Copy(tf, r)
	if err != nil {
		tf.Close()
		os.Remove(tf.Name())
		return 0, err
	}
	if err := tf.Close(); err != nil {
		os.Remove(tf.Name())
		return 0, err
	}
	if err := os.Rename(tf.Name(), dest); err != nil {
		os.Remove(tf.Name())
		return 0, err
	}
	return size, nil
}
