// Package testutil provides testing utilities for controller tests.
package envtestutil

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FailureConfig configures when the fake client should return errors.
// Each field is a function that receives the object/key and returns an error if the operation should fail.
type FailureConfig struct {
	// OnGet is called before Get operations. Return non-nil to fail the operation.
	OnGet func(key client.ObjectKey) error

	// OnList is called before List operations. Return non-nil to fail the operation.
	OnList func(list client.ObjectList) error

	// OnCreate is called before Create operations. Return non-nil to fail the operation.
	OnCreate func(obj client.Object) error

	// OnUpdate is called before Update operations. Return non-nil to fail the operation.
	OnUpdate func(obj client.Object) error

	// OnPatch is called before Patch operations. Return non-nil to fail the operation.
	OnPatch func(obj client.Object) error

	// OnDelete is called before Delete operations. Return non-nil to fail the operation.
	OnDelete func(obj client.Object) error

	// OnDeleteAllOf is called before DeleteAllOf operations. Return non-nil to fail the operation.
	OnDeleteAllOf func(obj client.Object) error

	// OnStatusUpdate is called before Status().Update() operations. Return non-nil to fail the operation.
	OnStatusUpdate func(obj client.Object) error

	// OnStatusPatch is called before Status().Patch() operations. Return non-nil to fail the operation.
	OnStatusPatch func(obj client.Object) error
}

// fakeClientWithFailures wraps a real fake client and injects failures based on configuration.
type fakeClientWithFailures struct {
	client.Client
	config *FailureConfig
}

// NewFakeClientWithFailures creates a fake client that can be configured to fail operations.
// This is useful for testing error handling paths in controllers.
func NewFakeClientWithFailures(baseClient client.Client, config *FailureConfig) client.Client {
	if config == nil {
		config = &FailureConfig{}
	}
	return &fakeClientWithFailures{
		Client: baseClient,
		config: config,
	}
}

func (c *fakeClientWithFailures) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if c.config.OnGet != nil {
		if err := c.config.OnGet(key); err != nil {
			return err
		}
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *fakeClientWithFailures) List(
	ctx context.Context,
	list client.ObjectList,
	opts ...client.ListOption,
) error {
	if c.config.OnList != nil {
		if err := c.config.OnList(list); err != nil {
			return err
		}
	}
	return c.Client.List(ctx, list, opts...)
}

func (c *fakeClientWithFailures) Create(
	ctx context.Context,
	obj client.Object,
	opts ...client.CreateOption,
) error {
	if c.config.OnCreate != nil {
		if err := c.config.OnCreate(obj); err != nil {
			return err
		}
	}
	return c.Client.Create(ctx, obj, opts...)
}

func (c *fakeClientWithFailures) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	if c.config.OnUpdate != nil {
		if err := c.config.OnUpdate(obj); err != nil {
			return err
		}
	}
	return c.Client.Update(ctx, obj, opts...)
}

func (c *fakeClientWithFailures) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	if c.config.OnPatch != nil {
		if err := c.config.OnPatch(obj); err != nil {
			return err
		}
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func (c *fakeClientWithFailures) Delete(
	ctx context.Context,
	obj client.Object,
	opts ...client.DeleteOption,
) error {
	if c.config.OnDelete != nil {
		if err := c.config.OnDelete(obj); err != nil {
			return err
		}
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func (c *fakeClientWithFailures) DeleteAllOf(
	ctx context.Context,
	obj client.Object,
	opts ...client.DeleteAllOfOption,
) error {
	if c.config.OnDeleteAllOf != nil {
		if err := c.config.OnDeleteAllOf(obj); err != nil {
			return err
		}
	}
	return c.Client.DeleteAllOf(ctx, obj, opts...)
}

func (c *fakeClientWithFailures) Status() client.StatusWriter {
	return &statusWriterWithFailures{
		StatusWriter: c.Client.Status(),
		config:       c.config,
	}
}

type statusWriterWithFailures struct {
	client.StatusWriter
	config *FailureConfig
}

func (s *statusWriterWithFailures) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.SubResourceUpdateOption,
) error {
	if s.config.OnStatusUpdate != nil {
		if err := s.config.OnStatusUpdate(obj); err != nil {
			return err
		}
	}
	return s.StatusWriter.Update(ctx, obj, opts...)
}

func (s *statusWriterWithFailures) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.SubResourcePatchOption,
) error {
	if s.config.OnStatusPatch != nil {
		if err := s.config.OnStatusPatch(obj); err != nil {
			return err
		}
	}
	return s.StatusWriter.Patch(ctx, obj, patch, opts...)
}

// Helper functions for common failure scenarios

// FailOnObjectName returns an error if the object name matches.
func FailOnObjectName(name string, err error) func(client.Object) error {
	return func(obj client.Object) error {
		accessor, metaErr := meta.Accessor(obj)
		if metaErr != nil {
			panic(fmt.Sprintf("meta.Accessor failed: %v", metaErr))
		}
		if accessor.GetName() == name {
			return err
		}
		return nil
	}
}

// FailOnKeyName returns an error if the key name matches.
func FailOnKeyName(name string, err error) func(client.ObjectKey) error {
	return func(key client.ObjectKey) error {
		if key.Name == name {
			return err
		}
		return nil
	}
}

// FailOnNamespacedKeyName returns an error if both the key name and namespace match.
func FailOnNamespacedKeyName(name, namespace string, err error) func(client.ObjectKey) error {
	return func(key client.ObjectKey) error {
		if key.Name == name && key.Namespace == namespace {
			return err
		}
		return nil
	}
}

// FailOnNamespace returns an error if the namespace matches.
func FailOnNamespace(namespace string, err error) func(client.Object) error {
	return func(obj client.Object) error {
		accessor, metaErr := meta.Accessor(obj)
		if metaErr != nil {
			panic(fmt.Sprintf("meta.Accessor failed: %v", metaErr))
		}
		if accessor.GetNamespace() == namespace {
			return err
		}
		return nil
	}
}

// AlwaysFail returns the given error for all operations.
func AlwaysFail(err error) func(any) error {
	return func(interface{}) error {
		return err
	}
}

// FailKeyAfterNCalls returns an ObjectKey failure function that fails after N successful calls.
// Use for OnGet.
func FailKeyAfterNCalls(n int, err error) func(client.ObjectKey) error {
	count := 0
	return func(client.ObjectKey) error {
		count++
		if count > n {
			return err
		}
		return nil
	}
}

// FailObjAfterNCalls returns an Object failure function that fails after N successful calls.
// Use for OnCreate, OnUpdate, OnDelete, OnPatch, OnDeleteAllOf, OnStatusUpdate, OnStatusPatch.
func FailObjAfterNCalls(n int, err error) func(client.Object) error {
	count := 0
	return func(client.Object) error {
		count++
		if count > n {
			return err
		}
		return nil
	}
}

// FailObjListAfterNCalls returns an ObjectList failure function that fails after N successful calls.
// Use for OnList.
func FailObjListAfterNCalls(n int, err error) func(client.ObjectList) error {
	count := 0
	return func(client.ObjectList) error {
		count++
		if count > n {
			return err
		}
		return nil
	}
}

// Common errors for testing
var (
	ErrInjected        = fmt.Errorf("injected test error")
	ErrNetworkTimeout  = fmt.Errorf("network timeout")
	ErrPermissionError = fmt.Errorf("permission denied")
)
