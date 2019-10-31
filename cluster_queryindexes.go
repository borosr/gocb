package gocb

import (
	"context"
	"strings"
	"time"
)

// QueryIndexManager provides methods for performing Couchbase N1ql index management.
// Volatile: This API is subject to change at any time.
type QueryIndexManager struct {
	executeQuery         func(requestSpanContext, string, time.Time, *QueryOptions) (*QueryResult, error)
	globalTimeout        time.Duration
	defaultRetryStrategy *retryStrategyWrapper
	tracer               requestTracer
}

// QueryIndex represents a Couchbase GSI index.
type QueryIndex struct {
	Name      string    `json:"name"`
	IsPrimary bool      `json:"is_primary"`
	Type      IndexType `json:"using"`
	State     string    `json:"state"`
	Keyspace  string    `json:"keyspace_id"`
	Namespace string    `json:"namespace_id"`
	IndexKey  []string  `json:"index_key"`
}

type createQueryIndexOptions struct {
	Context       context.Context
	RetryStrategy RetryStrategy

	IgnoreIfExists bool
	Deferred       bool
}

func (qm *QueryIndexManager) createIndexWhere(tracectx requestSpanContext, bucketName, indexName string, fields []string,
	startTime time.Time, opts createQueryIndexOptions, condition string) error {
	var qs string

	if len(fields) == 0 {
		qs += "CREATE PRIMARY INDEX"
	} else {
		qs += "CREATE INDEX"
	}
	if indexName != "" {
		qs += " `" + indexName + "`"
	}
	qs += " ON `" + bucketName + "`"
	if len(fields) > 0 {
		qs += " ("
		for i := 0; i < len(fields); i++ {
			if i > 0 {
				qs += ", "
			}
			qs += "`" + fields[i] + "`"
		}
		qs += ")"
	}
	if condition != "" {
		qs += " WHERE " + condition
	}
	if opts.Deferred {
		qs += " WITH {\"defer_build\": true}"
	}

	rows, err := qm.executeQuery(tracectx, qs, startTime, &QueryOptions{
		RetryStrategy: opts.RetryStrategy,
		Context:       opts.Context,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exist") {
			if opts.IgnoreIfExists {
				return nil
			}
			return queryIndexError{
				statusCode: 409,
				message:    err.Error(),
			}
		}
		return err
	}

	return rows.Close()
}

// CreateQueryIndexOptions is the set of options available to the query indexes CreateIndex operation.
type CreateQueryIndexOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy

	IgnoreIfExists bool
	Deferred       bool
}

// CreateIndex creates an index over the specified fields.
func (qm *QueryIndexManager) CreateIndex(bucketName, indexName string, fields []string, opts *CreateQueryIndexOptions) error {
	if indexName == "" {
		return invalidArgumentsError{
			message: "an invalid index name was specified",
		}
	}
	if len(fields) <= 0 {
		return invalidArgumentsError{
			message: "you must specify at least one field to index",
		}
	}

	startTime := time.Now()
	if opts == nil {
		opts = &CreateQueryIndexOptions{}
	}

	span := qm.tracer.StartSpan("CreateIndex", nil).
		SetTag("couchbase.service", "n1ql")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, qm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	return qm.createIndexWhere(span.Context(), bucketName, indexName, fields, startTime, createQueryIndexOptions{
		IgnoreIfExists: opts.IgnoreIfExists,
		Deferred:       opts.Deferred,
		Context:        ctx,
		RetryStrategy:  opts.RetryStrategy,
	}, "")
}

// CreateIndexWhere creates an index over the specified fields with where condition and params
func (qm *QueryIndexManager) CreateIndexWhere(bucketName, indexName string, fields []string, opts *CreateQueryIndexOptions, whereFormat string) error {
	if indexName == "" {
		return invalidArgumentsError{
			message: "an invalid index name was specified",
		}
	}
	if len(fields) <= 0 {
		return invalidArgumentsError{
			message: "you must specify at least one field to index",
		}
	}

	startTime := time.Now()
	if opts == nil {
		opts = &CreateQueryIndexOptions{}
	}

	span := qm.tracer.StartSpan("CreateIndex", nil).
		SetTag("couchbase.service", "n1ql")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, qm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	return qm.createIndexWhere(span.Context(), bucketName, indexName, fields, startTime,
		createQueryIndexOptions{
		IgnoreIfExists: opts.IgnoreIfExists,
		Deferred:       opts.Deferred,
		Context:        ctx,
	}, whereFormat)
}

// CreatePrimaryQueryIndexOptions is the set of options available to the query indexes CreatePrimaryIndex operation.
type CreatePrimaryQueryIndexOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy

	IgnoreIfExists bool
	Deferred       bool
	CustomName     string
}

// CreatePrimaryIndex creates a primary index.  An empty customName uses the default naming.
func (qm *QueryIndexManager) CreatePrimaryIndex(bucketName string, opts *CreatePrimaryQueryIndexOptions) error {
	startTime := time.Now()
	if opts == nil {
		opts = &CreatePrimaryQueryIndexOptions{}
	}

	span := qm.tracer.StartSpan("CreatePrimaryIndex", nil).
		SetTag("couchbase.service", "n1ql")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, qm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	return qm.createIndexWhere(span.Context(), bucketName, opts.CustomName, nil, startTime, createQueryIndexOptions{
		IgnoreIfExists: opts.IgnoreIfExists,
		Deferred:       opts.Deferred,
		Context:        ctx,
		RetryStrategy:  opts.RetryStrategy,
	}, "")
}

type dropQueryIndexOptions struct {
	Context       context.Context
	RetryStrategy RetryStrategy

	IgnoreIfNotExists bool
}

func (qm *QueryIndexManager) dropIndex(tracectx requestSpanContext, bucketName, indexName string, startTime time.Time,
	opts dropQueryIndexOptions) error {
	var qs string

	if indexName == "" {
		qs += "DROP PRIMARY INDEX ON `" + bucketName + "`"
	} else {
		qs += "DROP INDEX `" + bucketName + "`.`" + indexName + "`"
	}

	rows, err := qm.executeQuery(tracectx, qs, startTime, &QueryOptions{
		Context:       opts.Context,
		RetryStrategy: opts.RetryStrategy,
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			if opts.IgnoreIfNotExists {
				return nil
			}
			return queryIndexError{
				indexMissing: true,
				message:      err.Error(),
			}
		}
		return err
	}

	return rows.Close()
}

// DropQueryIndexOptions is the set of options available to the query indexes DropIndex operation.
type DropQueryIndexOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy

	IgnoreIfNotExists bool
}

// DropIndex drops a specific index by name.
func (qm *QueryIndexManager) DropIndex(bucketName, indexName string, opts *DropQueryIndexOptions) error {
	if indexName == "" {
		return invalidArgumentsError{
			message: "an invalid index name was specified",
		}
	}

	startTime := time.Now()
	if opts == nil {
		opts = &DropQueryIndexOptions{}
	}

	span := qm.tracer.StartSpan("DropIndex", nil).
		SetTag("couchbase.service", "n1ql")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, qm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	return qm.dropIndex(span.Context(), bucketName, indexName, startTime, dropQueryIndexOptions{
		Context:           ctx,
		IgnoreIfNotExists: opts.IgnoreIfNotExists,
		RetryStrategy:     opts.RetryStrategy,
	})
}

// DropPrimaryQueryIndexOptions is the set of options available to the query indexes DropPrimaryIndex operation.
type DropPrimaryQueryIndexOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy

	IgnoreIfNotExists bool
	CustomName        string
}

// DropPrimaryIndex drops the primary index.  Pass an empty customName for unnamed primary indexes.
func (qm *QueryIndexManager) DropPrimaryIndex(bucketName string, opts *DropPrimaryQueryIndexOptions) error {
	startTime := time.Now()
	if opts == nil {
		opts = &DropPrimaryQueryIndexOptions{}
	}

	span := qm.tracer.StartSpan("DropPrimaryIndex", nil).
		SetTag("couchbase.service", "n1ql")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, qm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	return qm.dropIndex(span.Context(), bucketName, opts.CustomName, startTime, dropQueryIndexOptions{
		IgnoreIfNotExists: opts.IgnoreIfNotExists,
		Context:           ctx,
		RetryStrategy:     opts.RetryStrategy,
	})
}

// GetAllQueryIndexesOptions is the set of options available to the query indexes GetAllIndexes operation.
type GetAllQueryIndexesOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy
}

// GetAllIndexes returns a list of all currently registered indexes.
func (qm *QueryIndexManager) GetAllIndexes(bucketName string, opts *GetAllQueryIndexesOptions) ([]QueryIndex, error) {
	if opts == nil {
		opts = &GetAllQueryIndexesOptions{}
	}

	span := qm.tracer.StartSpan("GetAllIndexes", nil).
		SetTag("couchbase.service", "n1ql")
	defer span.Finish()

	return qm.getAllIndexes(span.Context(), bucketName, time.Now(), opts)
}

func (qm *QueryIndexManager) getAllIndexes(tracectx requestSpanContext, bucketName string, startTime time.Time,
	opts *GetAllQueryIndexesOptions) ([]QueryIndex, error) {

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, qm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	q := "SELECT `indexes`.* FROM system:indexes WHERE keyspace_id=?"
	queryOpts := &QueryOptions{
		Context:              ctx,
		PositionalParameters: []interface{}{bucketName},
		RetryStrategy:        opts.RetryStrategy,
		ReadOnly:             true,
	}

	rows, err := qm.executeQuery(tracectx, q, startTime, queryOpts)
	if err != nil {
		return nil, err
	}

	var indexes []QueryIndex
	var index QueryIndex
	for rows.Next(&index) {
		indexes = append(indexes, index)
		index = QueryIndex{}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	return indexes, nil
}

// BuildDeferredQueryIndexOptions is the set of options available to the query indexes BuildDeferredIndexes operation.
type BuildDeferredQueryIndexOptions struct {
	Timeout       time.Duration
	Context       context.Context
	RetryStrategy RetryStrategy
}

// BuildDeferredIndexes builds all indexes which are currently in deferred state.
func (qm *QueryIndexManager) BuildDeferredIndexes(bucketName string, opts *BuildDeferredQueryIndexOptions) ([]string, error) {
	startTime := time.Now()
	if opts == nil {
		opts = &BuildDeferredQueryIndexOptions{}
	}

	span := qm.tracer.StartSpan("BuildDeferredIndexes", nil).
		SetTag("couchbase.service", "n1ql")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(opts.Context, opts.Timeout, qm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	indexList, err := qm.getAllIndexes(span.Context(), bucketName, startTime, &GetAllQueryIndexesOptions{
		Context:       ctx,
		RetryStrategy: opts.RetryStrategy,
	})
	if err != nil {
		return nil, err
	}

	var deferredList []string
	for i := 0; i < len(indexList); i++ {
		var index = indexList[i]
		if index.State == "deferred" || index.State == "pending" {
			deferredList = append(deferredList, index.Name)
		}
	}

	if len(deferredList) == 0 {
		// Don't try to build an empty index list
		return nil, nil
	}

	var qs string
	qs += "BUILD INDEX ON `" + bucketName + "`("
	for i := 0; i < len(deferredList); i++ {
		if i > 0 {
			qs += ", "
		}
		qs += "`" + deferredList[i] + "`"
	}
	qs += ")"

	rows, err := qm.executeQuery(span.Context(), qs, startTime, &QueryOptions{
		Context:       ctx,
		RetryStrategy: opts.RetryStrategy,
	})
	if err != nil {
		return nil, err
	}

	if err := rows.Close(); err != nil {
		return nil, err
	}

	return deferredList, nil
}

func checkIndexesActive(indexes []QueryIndex, checkList []string) (bool, error) {
	var checkIndexes []QueryIndex
	for i := 0; i < len(checkList); i++ {
		indexName := checkList[i]

		for j := 0; j < len(indexes); j++ {
			if indexes[j].Name == indexName {
				checkIndexes = append(checkIndexes, indexes[j])
				break
			}
		}
	}

	if len(checkIndexes) != len(checkList) {
		return false, queryIndexError{
			indexMissing: true,
			message:      "the index specified does not exist",
		}
	}

	for i := 0; i < len(checkIndexes); i++ {
		if checkIndexes[i].State != "online" {
			return false, nil
		}
	}
	return true, nil
}

// WatchQueryIndexOptions is the set of options available to the query indexes Watch operation.
type WatchQueryIndexOptions struct {
	WatchPrimary  bool
	RetryStrategy RetryStrategy
}

// WatchQueryIndexTimeout is used for setting a timeout value for the query indexes WatchIndexes operation.
type WatchQueryIndexTimeout struct {
	Timeout time.Duration
	Context context.Context
}

// WatchIndexes waits for a set of indexes to come online.
func (qm *QueryIndexManager) WatchIndexes(bucketName string, watchList []string, timeout WatchQueryIndexTimeout, opts *WatchQueryIndexOptions) error {
	startTime := time.Now()
	if timeout.Context == nil && timeout.Timeout == 0 {
		return invalidArgumentsError{
			message: "either a context or a timeout value must be supplied to watch",
		}
	}

	if opts == nil {
		opts = &WatchQueryIndexOptions{}
	}

	span := qm.tracer.StartSpan("WatchIndexes", nil).
		SetTag("couchbase.service", "n1ql")
	defer span.Finish()

	ctx, cancel := contextFromMaybeTimeout(timeout.Context, timeout.Timeout, qm.globalTimeout)
	if cancel != nil {
		defer cancel()
	}

	if opts.WatchPrimary {
		watchList = append(watchList, "#primary")
	}

	curInterval := 50 * time.Millisecond
	for {
		indexes, err := qm.getAllIndexes(span.Context(), bucketName, startTime, &GetAllQueryIndexesOptions{
			Context:       ctx,
			RetryStrategy: opts.RetryStrategy,
		})
		if err != nil {
			return err
		}

		allOnline, err := checkIndexesActive(indexes, watchList)
		if err != nil {
			return err
		}

		if allOnline {
			break
		}

		curInterval += 500 * time.Millisecond
		if curInterval > 1000 {
			curInterval = 1000
		}

		// This can only be !ok if the user has set context to something like Background so let's just keep running.
		d, ok := ctx.Deadline()
		if ok {
			if time.Now().Add(curInterval).After(d) {
				return timeoutError{}
			}
		}

		// wait till our next poll interval
		time.Sleep(curInterval)
	}

	return nil
}
