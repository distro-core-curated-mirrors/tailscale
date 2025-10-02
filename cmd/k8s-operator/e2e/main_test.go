// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package e2e

import (
	"context"
	"flag"
	"log"
	"os"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This test suite is currently not run in CI.
// It requires some setup not handled by this code:
// - Kubernetes cluster with local kubeconfig for it (direct connection, no API server proxy)
// - Tailscale operator installed with --set apiServerProxyConfig.mode="true"
// - ACLs from acl.hujson
// - OAuth client secret in TS_API_CLIENT_SECRET env, with at least auth_keys write scope and tag:k8s tag
func TestMain(m *testing.M) {
	flag.Parse()
	code, err := runTests(m)
	if err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func objectMeta(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
	}
}

func createAndCleanup(t *testing.T, cl client.Client, obj client.Object) {
	t.Helper()

	// Try to create the object first
	err := cl.Create(t.Context(), obj)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			if updateErr := cl.Update(t.Context(), obj); updateErr != nil {
				t.Fatal(updateErr)
			}
		} else {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		// Use context.Background() for cleanup, as t.Context() is cancelled
		// just before cleanup functions are called.
		if err := cl.Delete(context.Background(), obj); err != nil {
			t.Errorf("error cleaning up %s %s/%s: %s", obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	})
}

func get(ctx context.Context, cl client.Client, obj client.Object) error {
	return cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)
}
