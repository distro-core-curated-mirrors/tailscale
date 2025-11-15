// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package ipnlocal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tailscale.com/feature/buildfeatures"
	"tailscale.com/types/netmap"
	"tailscale.com/util/mak"
)

// diskCache is the state netmap caching to disk.
type diskCache struct {
	// all fields guarded by LocalBackend.mu

	dir       string               // active directory to write to
	lastWrote map[string]lastWrote // base name => contents written
}

type lastWrote struct {
	baseDir  string
	contents []byte
	at       time.Time
}

func (dc *diskCache) writeJSON(baseName string, v any) error {
	j, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("errror JSON marshalling %q: %w", baseName, err)
	}
	last, ok := dc.lastWrote[baseName]
	if ok && last.baseDir == dc.dir && bytes.Equal(j, last.contents) {
		// Avoid disk writes
		return nil
	}
	err = os.WriteFile(filepath.Join(dc.dir, baseName), j, 0600)
	if err != nil {
		return err
	}
	mak.Set(&dc.lastWrote, baseName, lastWrote{
		baseDir:  dc.dir,
		contents: j,
		at:       time.Now(),
	})
	return nil
}

func (b *LocalBackend) writeNetmapToDiskLocked(nm *netmap.NetworkMap) error {
	if nm == nil {
		return nil
	}
	prof, ok := nm.UserProfiles[nm.User()]
	if !ok {
		return errors.New("no profile for current user in netmap")
	}
	root := b.varRoot
	if root == "" {
		return errors.New("no varRoot")
	}

	dc := &b.diskCache
	// TODO(bradfitz): the (ID integer, LoginName string) tuple is not sufficiently
	// globally unique. It doesn't include the control plane server URL. We should
	// make each profile have a local UUID.
	dc.dir = filepath.Join(root, fmt.Sprintf("nm-%d-%s", prof.ID(), prof.LoginName()))

	if err := os.MkdirAll(dc.dir, 0700); err != nil {
		return err
	}

	if buildfeatures.HasSSH {
		if err := dc.writeJSON("ssh", nm.SSHPolicy); err != nil {
			return err
		}
	}
	if err := dc.writeJSON("dns", nm.DNS); err != nil {
		return err
	}
	if err := dc.writeJSON("derpmap", nm.DERPMap); err != nil {
		return err
	}
	if err := dc.writeJSON("self", nm.SelfNode); err != nil {
		return err
	}
	for _, p := range nm.Peers {
		if err := dc.writeJSON("peer-"+string(p.StableID()), p); err != nil {
			return err
		}
	}
	for uid, p := range nm.UserProfiles {
		if err := dc.writeJSON(fmt.Sprintf("user-%d", uid), p); err != nil {
			return err
		}
	}
	return nil
}
