# AWS CloudMap registry for go-micro

A registry plugin for [go-micro](https://github.com/micro/go-micro) to compliment a fully managed, AWS-native deployment.

## Usage

The plugin requires either a NamespaceID or Domain option to select an AWS CloudMap namespace.*

```golang
	// New Service
	service := micro.NewService(
		micro.Name("go.micro.srv.thing"),
		micro.Version("latest"),
		micro.Registry(cloudmap.NewRegistry(
			cloudmap.NamespaceID("ns-abc123def456"),
			cloudmap.Domain("myzone.int"))),
	)
```

_As of this publishing, both are required. Future TODO will swap to enabling either option by itself._

## Gotchas

There are some naming conflicts with the default HTTP broker. Topic's are worked around, but the broker registration is still pending a fix.

Watcher is built, but mostly untested. There's bound to be ugly bugs hiding in there.