package gocb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// ViewScanConsistency specifies the consistency required for a view query.
type ViewScanConsistency int

const (
	// ViewScanConsistencyNotBounded indicates that no special behaviour should be used.
	ViewScanConsistencyNotBounded = ViewScanConsistency(1)
	// ViewScanConsistencyRequestPlus indicates to update the index before querying it.
	ViewScanConsistencyRequestPlus = ViewScanConsistency(2)
	// ViewScanConsistencyUpdateAfter indicates to update the index asynchronously after querying.
	ViewScanConsistencyUpdateAfter = ViewScanConsistency(3)
)

// ViewOrdering specifies the ordering for the view queries results.
type ViewOrdering int

const (
	// ViewOrderingAscending indicates the query results should be sorted from lowest to highest.
	ViewOrderingAscending = ViewOrdering(1)
	// ViewOrderingDescending indicates the query results should be sorted from highest to lowest.
	ViewOrderingDescending = ViewOrdering(2)
)

// ViewErrorMode pecifies the behaviour of the query engine should an error occur during the gathering of
// view index results which would result in only partial results being available.
type ViewErrorMode int

const (
	// ViewErrorModeContinue indicates to continue gathering results on error.
	ViewErrorModeContinue = ViewErrorMode(1)

	// ViewErrorModeStop indicates to stop gathering results on error
	ViewErrorModeStop = ViewErrorMode(2)
)

// ViewOptions represents the options available when executing view query.
type ViewOptions struct {
	ScanConsistency ViewScanConsistency
	Skip            uint
	Limit           uint
	Order           ViewOrdering
	Reduce          bool
	Group           bool
	GroupLevel      uint
	Key             interface{}
	Keys            []interface{}
	StartKey        interface{}
	EndKey          interface{}
	InclusiveEnd    bool
	StartKeyDocID   string
	EndKeyDocID     string
	Namespace       DesignDocumentNamespace
	Raw             map[string]string
	// Timeout and context are used to control cancellation of the data stream.
	Context context.Context
	Timeout time.Duration
	OnError ViewErrorMode
	Debug   bool

	// JSONSerializer is used to deserialize each row in the result. This should be a JSON deserializer as results are JSON.
	// NOTE: if not set then views will always default to DefaultJSONSerializer.
	Serializer JSONSerializer

	RetryStrategy RetryStrategy
}

func (opts *ViewOptions) toURLValues() (*url.Values, error) {
	options := &url.Values{}

	if opts.ScanConsistency != 0 {
		if opts.ScanConsistency == ViewScanConsistencyRequestPlus {
			options.Set("stale", "false")
		} else if opts.ScanConsistency == ViewScanConsistencyNotBounded {
			options.Set("stale", "ok")
		} else if opts.ScanConsistency == ViewScanConsistencyUpdateAfter {
			options.Set("stale", "update_after")
		} else {
			return nil, invalidArgumentsError{message: "unexpected stale option"}
		}
	}

	if opts.Skip != 0 {
		options.Set("skip", strconv.FormatUint(uint64(opts.Skip), 10))
	}

	if opts.Limit != 0 {
		options.Set("limit", strconv.FormatUint(uint64(opts.Limit), 10))
	}

	if opts.Order != 0 {
		if opts.Order == ViewOrderingAscending {
			options.Set("descending", "false")
		} else if opts.Order == ViewOrderingDescending {
			options.Set("descending", "true")
		} else {
			return nil, invalidArgumentsError{message: "unexpected order option"}
		}
	}

	options.Set("reduce", "false") // is this line necessary?
	if opts.Reduce {
		options.Set("reduce", "true")

		// Only set group if a reduce view
		options.Set("group", "false") // is this line necessary?
		if opts.Group {
			options.Set("group", "true")
		}

		if opts.GroupLevel != 0 {
			options.Set("group_level", strconv.FormatUint(uint64(opts.GroupLevel), 10))
		}
	}

	if opts.Key != nil {
		jsonKey, err := opts.marshalJson(opts.Key)
		if err != nil {
			return nil, err
		}
		options.Set("key", string(jsonKey))
	}

	if len(opts.Keys) > 0 {
		jsonKeys, err := opts.marshalJson(opts.Keys)
		if err != nil {
			return nil, err
		}
		options.Set("keys", string(jsonKeys))
	}

	if opts.StartKey != nil {
		jsonStartKey, err := opts.marshalJson(opts.StartKey)
		if err != nil {
			return nil, err
		}
		options.Set("startkey", string(jsonStartKey))
	} else {
		options.Del("startkey")
	}

	if opts.EndKey != nil {
		jsonEndKey, err := opts.marshalJson(opts.EndKey)
		if err != nil {
			return nil, err
		}
		options.Set("endkey", string(jsonEndKey))
	} else {
		options.Del("endkey")
	}

	if opts.StartKey != nil || opts.EndKey != nil {
		if opts.InclusiveEnd {
			options.Set("inclusive_end", "true")
		} else {
			options.Set("inclusive_end", "false")
		}
	}

	if opts.StartKeyDocID == "" {
		options.Del("startkey_docid")
	} else {
		options.Set("startkey_docid", opts.StartKeyDocID)
	}

	if opts.EndKeyDocID == "" {
		options.Del("endkey_docid")
	} else {
		options.Set("endkey_docid", opts.EndKeyDocID)
	}

	if opts.OnError > 0 {
		if opts.OnError == ViewErrorModeContinue {
			options.Set("on_error", "continue")
		} else if opts.OnError == ViewErrorModeStop {
			options.Set("on_error", "stop")
		} else {
			return nil, invalidArgumentsError{"unexpected onerror option"}
		}
	}

	if opts.Debug {
		options.Set("debug", "true")
	}

	if opts.Raw != nil {
		for k, v := range opts.Raw {
			options.Set(k, v)
		}
	}

	return options, nil
}

func (opts *ViewOptions) marshalJson(value interface{}) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(value)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
