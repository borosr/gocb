package gocb

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pkg/errors"

	gocbcore "github.com/couchbase/gocbcore/v8"
)

// DesignDocumentNamespace represents which namespace a design document resides in.
type DesignDocumentNamespace bool

const (
	// ProductionDesignDocumentNamespace means that a design document resides in the production namespace.
	ProductionDesignDocumentNamespace = true

	// DevelopmentDesignDocumentNamespace means that a design document resides in the development namespace.
	DevelopmentDesignDocumentNamespace = false
)

// ViewIndexManager provides methods for performing View management.
// Volatile: This API is subject to change at any time.
type ViewIndexManager struct {
	bucketName           string
	httpClient           httpProvider
	globalTimeout        time.Duration
	defaultRetryStrategy *retryStrategyWrapper
	tracer               requestTracer
}

// View represents a Couchbase view within a design document.
type View struct {
	Map    string `json:"map,omitempty"`
	Reduce string `json:"reduce,omitempty"`
}

func (v View) hasReduce() bool {
	return v.Reduce != ""
}

// DesignDocument represents a Couchbase design document containing multiple views.
type DesignDocument struct {
	Name  string          `json:"-"`
	Views map[string]View `json:"views,omitempty"`
}

// GetDesignDocumentOptions is the set of options available to the ViewIndexManager GetDesignDocument operation.
type GetDesignDocumentOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy
}

func (vm *ViewIndexManager) ddocName(name string, isProd DesignDocumentNamespace) string {
	if isProd {
		if strings.HasPrefix(name, "dev_") {
			name = strings.TrimLeft(name, "dev_")
		}
	} else {
		if !strings.HasPrefix(name, "dev_") {
			name = "dev_" + name
		}
	}

	return name
}

// GetDesignDocument retrieves a single design document for the given bucket.
func (vm *ViewIndexManager) GetDesignDocument(name string, namespace DesignDocumentNamespace, opts *GetDesignDocumentOptions) (*DesignDocument, error) {
	if opts == nil {
		opts = &GetDesignDocumentOptions{}
	}

	span := vm.tracer.StartSpan("GetDesignDocument", nil).SetTag("couchbase.service", "view")
	defer span.Finish()

	return vm.getDesignDocument(span.Context(), name, namespace, time.Now(), opts)
}

func (vm *ViewIndexManager) getDesignDocument(tracectx requestSpanContext, name string, namespace DesignDocumentNamespace,
	startTime time.Time, opts *GetDesignDocumentOptions) (*DesignDocument, error) {

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, vm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	name = vm.ddocName(name, namespace)

	retryStrategy := vm.defaultRetryStrategy
	if opts.RetryStrategy == nil {
		retryStrategy = newRetryStrategyWrapper(opts.RetryStrategy)
	}

	req := &gocbcore.HttpRequest{
		Service:       gocbcore.ServiceType(CapiService),
		Path:          fmt.Sprintf("/_design/%s", name),
		Method:        "GET",
		Context:       ctx,
		IsIdempotent:  true,
		RetryStrategy: retryStrategy,
		UniqueId:      uuid.New().String(),
	}

	dspan := vm.tracer.StartSpan("dispatch", tracectx)
	resp, err := vm.httpClient.DoHttpRequest(req)
	dspan.Finish()
	if err != nil {
		if err == context.DeadlineExceeded {
			return nil, timeoutError{
				operationID:   req.UniqueId,
				retryReasons:  req.RetryReasons(),
				retryAttempts: req.RetryAttempts(),
				operation:     "view",
				elapsed:       time.Now().Sub(startTime),
			}
		}

		return nil, err
	}

	if resp.StatusCode != 200 {
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		err = resp.Body.Close()
		if err != nil {
			logDebugf("Failed to close socket (%s)", err)
		}

		return nil, viewIndexError{
			statusCode:   resp.StatusCode,
			message:      string(data),
			indexMissing: resp.StatusCode == 404,
		}
	}

	ddocObj := DesignDocument{}
	jsonDec := json.NewDecoder(resp.Body)
	err = jsonDec.Decode(&ddocObj)
	if err != nil {
		return nil, err
	}

	ddocObj.Name = strings.TrimPrefix(name, "dev_")
	return &ddocObj, nil
}

// GetAllDesignDocumentsOptions is the set of options available to the ViewIndexManager GetAllDesignDocuments operation.
type GetAllDesignDocumentsOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy
}

// GetAllDesignDocuments will retrieve all design documents for the given bucket.
func (vm *ViewIndexManager) GetAllDesignDocuments(namespace DesignDocumentNamespace, opts *GetAllDesignDocumentsOptions) ([]*DesignDocument, error) {
	startTime := time.Now()
	if opts == nil {
		opts = &GetAllDesignDocumentsOptions{}
	}

	span := vm.tracer.StartSpan("GetAllDesignDocuments", nil).SetTag("couchbase.service", "view")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, vm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	retryStrategy := vm.defaultRetryStrategy
	if opts.RetryStrategy == nil {
		retryStrategy = newRetryStrategyWrapper(opts.RetryStrategy)
	}

	req := &gocbcore.HttpRequest{
		Service:       gocbcore.ServiceType(MgmtService),
		Path:          fmt.Sprintf("/pools/default/buckets/%s/ddocs", vm.bucketName),
		Method:        "GET",
		Context:       ctx,
		IsIdempotent:  true,
		RetryStrategy: retryStrategy,
	}

	espan := vm.tracer.StartSpan("encode", span.Context())
	resp, err := vm.httpClient.DoHttpRequest(req)
	espan.Finish()
	if err != nil {
		if err == context.DeadlineExceeded {
			return nil, timeoutError{
				operationID:   req.UniqueId,
				retryReasons:  req.RetryReasons(),
				retryAttempts: req.RetryAttempts(),
				operation:     "view",
				elapsed:       time.Now().Sub(startTime),
			}
		}

		return nil, err
	}

	if resp.StatusCode != 200 {
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		err = resp.Body.Close()
		if err != nil {
			logDebugf("Failed to close socket (%s)", err)
		}
		return nil, viewIndexError{statusCode: resp.StatusCode, message: string(data)}
	}

	var ddocsObj struct {
		Rows []struct {
			Doc struct {
				Meta struct {
					Id string
				}
				Json DesignDocument
			}
		}
	}
	jsonDec := json.NewDecoder(resp.Body)
	err = jsonDec.Decode(&ddocsObj)
	if err != nil {
		return nil, err
	}

	var ddocs []*DesignDocument
	for index, ddocData := range ddocsObj.Rows {
		ddoc := &ddocsObj.Rows[index].Doc.Json
		isProd := !strings.HasPrefix(ddoc.Name, "dev_")
		if isProd == bool(namespace) {
			ddoc.Name = strings.TrimPrefix(ddocData.Doc.Meta.Id[8:], "dev_")
			ddocs = append(ddocs, ddoc)
		}
	}

	return ddocs, nil
}

// UpsertDesignDocumentOptions is the set of options available to the ViewIndexManager UpsertDesignDocument operation.
type UpsertDesignDocumentOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy
}

// UpsertDesignDocument will insert a design document to the given bucket, or update
// an existing design document with the same name.
func (vm *ViewIndexManager) UpsertDesignDocument(ddoc DesignDocument, namespace DesignDocumentNamespace, opts *UpsertDesignDocumentOptions) error {
	if opts == nil {
		opts = &UpsertDesignDocumentOptions{}
	}

	span := vm.tracer.StartSpan("UpsertDesignDocument", nil).SetTag("couchbase.service", "view")
	defer span.Finish()

	return vm.upsertDesignDocument(span.Context(), ddoc, namespace, time.Now(), opts)
}

func (vm *ViewIndexManager) upsertDesignDocument(tracectx requestSpanContext, ddoc DesignDocument, namespace DesignDocumentNamespace, startTime time.Time,
	opts *UpsertDesignDocumentOptions) error {
	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, vm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	espan := vm.tracer.StartSpan("encode", tracectx)
	data, err := json.Marshal(&ddoc)
	espan.Finish()
	if err != nil {
		return err
	}

	ddoc.Name = vm.ddocName(ddoc.Name, namespace)

	retryStrategy := vm.defaultRetryStrategy
	if opts.RetryStrategy == nil {
		retryStrategy = newRetryStrategyWrapper(opts.RetryStrategy)
	}

	req := &gocbcore.HttpRequest{
		Service:       gocbcore.ServiceType(CapiService),
		Path:          fmt.Sprintf("/_design/%s", ddoc.Name),
		Method:        "PUT",
		Body:          data,
		Context:       ctx,
		RetryStrategy: retryStrategy,
	}

	dspan := vm.tracer.StartSpan("dispatch", nil)
	resp, err := vm.httpClient.DoHttpRequest(req)
	dspan.Finish()
	if err != nil {
		if err == context.DeadlineExceeded {
			return timeoutError{
				operationID:   req.UniqueId,
				retryReasons:  req.RetryReasons(),
				retryAttempts: req.RetryAttempts(),
				operation:     "view",
				elapsed:       time.Now().Sub(startTime),
			}
		}

		return err
	}

	if resp.StatusCode != 201 {
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		err = resp.Body.Close()
		if err != nil {
			logDebugf("Failed to close socket (%s)", err)
		}
		return viewIndexError{statusCode: resp.StatusCode, message: string(data)}
	}

	return nil
}

// DropDesignDocumentOptions is the set of options available to the ViewIndexManager Upsert operation.
type DropDesignDocumentOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy
}

// DropDesignDocument will remove a design document from the given bucket.
func (vm *ViewIndexManager) DropDesignDocument(name string, namespace DesignDocumentNamespace, opts *DropDesignDocumentOptions) error {
	if opts == nil {
		opts = &DropDesignDocumentOptions{}
	}

	span := vm.tracer.StartSpan("DropDesignDocument", nil).SetTag("couchbase.service", "view")
	defer span.Finish()

	return vm.dropDesignDocument(span.Context(), name, namespace, time.Now(), opts)
}

func (vm *ViewIndexManager) dropDesignDocument(tracectx requestSpanContext, name string, namespace DesignDocumentNamespace,
	startTime time.Time, opts *DropDesignDocumentOptions) error {
	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, vm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	name = vm.ddocName(name, namespace)

	retryStrategy := vm.defaultRetryStrategy
	if opts.RetryStrategy == nil {
		retryStrategy = newRetryStrategyWrapper(opts.RetryStrategy)
	}

	req := &gocbcore.HttpRequest{
		Service:       gocbcore.ServiceType(CapiService),
		Path:          fmt.Sprintf("/_design/%s", name),
		Method:        "DELETE",
		Context:       ctx,
		RetryStrategy: retryStrategy,
	}

	dspan := vm.tracer.StartSpan("dispatch", tracectx)
	resp, err := vm.httpClient.DoHttpRequest(req)
	dspan.Finish()
	if err != nil {
		if err == context.DeadlineExceeded {
			return timeoutError{
				operationID:   req.UniqueId,
				retryReasons:  req.RetryReasons(),
				retryAttempts: req.RetryAttempts(),
				operation:     "view",
				elapsed:       time.Now().Sub(startTime),
			}
		}

		return err
	}

	if resp.StatusCode != 200 {
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		err = resp.Body.Close()
		if err != nil {
			logDebugf("Failed to close socket (%s)", err)
		}
		return viewIndexError{
			statusCode:   resp.StatusCode,
			message:      string(data),
			indexMissing: resp.StatusCode == 404,
		}
	}

	return nil
}

// PublishDesignDocumentOptions is the set of options available to the ViewIndexManager PublishDesignDocument operation.
type PublishDesignDocumentOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy
}

// PublishDesignDocument publishes a design document to the given bucket.
func (vm *ViewIndexManager) PublishDesignDocument(name string, opts *PublishDesignDocumentOptions) error {
	startTime := time.Now()
	if opts == nil {
		opts = &PublishDesignDocumentOptions{}
	}

	span := vm.tracer.StartSpan("PublishDesignDocument", nil).
		SetTag("couchbase.service", "view")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, vm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	devdoc, err := vm.getDesignDocument(span.Context(), name, false, startTime, &GetDesignDocumentOptions{
		Context:       ctx,
		RetryStrategy: opts.RetryStrategy,
	})
	if err != nil {
		indexErr, ok := err.(viewIndexError)
		if ok {
			if indexErr.indexMissing {
				return viewIndexError{message: "Development design document does not exist", indexMissing: true}
			}
		}
		return err
	}

	err = vm.upsertDesignDocument(span.Context(), *devdoc, true, startTime, &UpsertDesignDocumentOptions{
		Context:       ctx,
		RetryStrategy: opts.RetryStrategy,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create ")
	}

	return nil
}
