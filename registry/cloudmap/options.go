package cloudmap

import (
	"context"
	"time"

	"github.com/micro/go-micro/registry"
)

type namespaceIDKey struct{}
type domainKey struct{}
type pollIntervalKey struct{}

func getNamespaceID(ctx context.Context) string {
	n, ok := ctx.Value(namespaceIDKey{}).(string)
	if !ok {
		return ""
	}
	return n
}

// NamespaceID is used to set the AWS CloudMap namespace by ID
func NamespaceID(n string) registry.Option {
	return func(o *registry.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}

		o.Context = context.WithValue(o.Context, namespaceIDKey{}, n)
	}
}

func getDomain(ctx context.Context) string {
	n, ok := ctx.Value(domainKey{}).(string)
	if !ok {
		return ""
	}
	return n
}

// Domain is used to select an AWS CloudMap namespace by domain name when the ID is unknown
func Domain(n string) registry.Option {
	return func(o *registry.Options) {
		if o.Context == nil {
			o.Context = context.Background()
		}

		o.Context = context.WithValue(o.Context, domainKey{}, n)
	}
}

const minimumPoll = 30 * time.Second
const defaultPoll = 1 * time.Minute

func getPollInterval(ctx context.Context) time.Duration {
	d, ok := ctx.Value(pollIntervalKey{}).(time.Duration)
	if !ok {
		return defaultPoll
	}

	if d < minimumPoll {
		return minimumPoll
	}

	return d
}

// PollInterval sets the frequency for the watcher to poll CloudMap, minimum 30s, default 1m
func PollInterval(d time.Duration) registry.WatchOption {
	return func(o *registry.WatchOptions) {
		if o.Context == nil {
			o.Context = context.Background()
		}

		o.Context = context.WithValue(o.Context, pollIntervalKey{}, d)
	}
}
