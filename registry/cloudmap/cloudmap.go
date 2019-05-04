package cloudmap

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	sd "github.com/aws/aws-sdk-go/service/servicediscovery"
	"github.com/micro/go-micro/cmd"
	"github.com/micro/go-micro/registry"
	hash "github.com/mitchellh/hashstructure"
)

var (
	// ErrNamespaceNotFound returned when the required namespace option is not provided
	ErrNamespaceNotFound = errors.New("namespace not found")
)

const (
	attrIP        = "AWS_INSTANCE_IPV4"
	attrPort      = "AWS_INSTANCE_PORT"
	attrEndpoints = "MICRO-ENDPOINTS"
	attrMetadata  = "MICRO-METADATA"
	attrVersion   = "MICRO-VERSION"
)

type cregistry struct {
	NamespaceID string
	Domain      string
	Client      *sd.ServiceDiscovery
	opts        registry.Options

	sync.Mutex
	register   map[string]uint64
	serviceIDs map[string]string
	// lastChecked tracks when a node was last checked as existing in ServiceDiscovery
	lastChecked map[string]time.Time
}

func init() {
	cmd.DefaultRegistries["cloudmap"] = NewRegistry
}

func configure(r *cregistry, opts ...registry.Option) error {
	for _, o := range opts {
		o(&r.opts)
	}

	r.NamespaceID = getNamespaceID(r.opts.Context)
	r.Domain = getDomain(r.opts.Context)

	if len(r.NamespaceID) == 0 || len(r.Domain) == 0 {
		// TODO handle namespace name provided instead of ID
		// this is a panic instead of the original err return, because the
		// micro binary ignores errors in Init :(

		panic("cloudmap-registry: can't be used without valid namespace options")
		// return ErrNamespaceNotFound
	}

	if r.Client == nil {
		s := session.Must(session.NewSession())
		r.Client = sd.New(s)
	}

	return nil
}

func (r *cregistry) Init(opts ...registry.Option) error {
	return configure(r, opts...)
}

func (r *cregistry) Options() registry.Options {
	return r.opts
}

func (r *cregistry) Register(s *registry.Service, opts ...registry.RegisterOption) error {
	if len(s.Nodes) == 0 {
		return errors.New("Require at least one node")
	}

	// TODO: fix 'topic:servicename' name validation error
	name := sanitizeServiceName(s.Name)

	// create hash of service; uint64
	h, err := hash.Hash(s, nil)
	if err != nil {
		return err
	}

	// use first node
	node := s.Nodes[0]

	// get existing hash and last checked time
	r.Lock()
	v, ok := r.register[name]
	serviceID, sok := r.serviceIDs[name]
	lastChecked := r.lastChecked[name]
	r.Unlock()

	// check if service already exists
	// service is known and matches the last hash, pass the health check
	if ok && v == h {
		// Update to a param for Interval
		if time.Since(lastChecked) < time.Minute {
			return nil
		}

		// TODO: pass the health check
		return nil
	}

	// create service if not present
	if !sok {
		var found bool
		serviceID, found = r.findServiceID(name)
		if !found {
			// create
			rsp, err := r.Client.CreateService(&sd.CreateServiceInput{
				CreatorRequestId: aws.String(string(time.Now().Unix())),
				Name:             aws.String(name),
				NamespaceId:      aws.String(r.NamespaceID),
				DnsConfig: &sd.DnsConfig{
					DnsRecords: []*sd.DnsRecord{
						&sd.DnsRecord{
							TTL:  aws.Int64(60),
							Type: aws.String("SRV"),
						},
					},
				},
			})

			if err != nil {
				// Failed creating the service, can't proceed
				return err
			}

			serviceID = aws.StringValue(rsp.Service.Id)
			r.serviceIDs[name] = serviceID
		}
	}

	// register this instance with the service
	attrs := make(map[string]*string)
	attrs[attrIP] = aws.String(node.Address)
	attrs[attrPort] = aws.String(strconv.Itoa(node.Port))
	attrs[attrEndpoints] = aws.String(encodeEndpoints(s.Endpoints))
	attrs[attrMetadata] = aws.String(encodeMetadata(node.Metadata))
	attrs[attrVersion] = aws.String(encodeVersion(s.Version))

	_, err = r.Client.RegisterInstance(&sd.RegisterInstanceInput{
		CreatorRequestId: aws.String(string(time.Now().Unix())),
		ServiceId:        aws.String(serviceID),
		InstanceId:       aws.String(node.Id),
		Attributes:       attrs,
	})

	if err != nil {
		// Registration failed
		return err
	}

	return nil
	// setup health checks (?)
}

func (r *cregistry) Deregister(s *registry.Service) error {
	if len(s.Nodes) == 0 {
		return errors.New("Require at least one node")
	}
	name := sanitizeServiceName(s.Name)

	// delete our hash and time check of the service
	r.Lock()
	delete(r.register, name)
	delete(r.lastChecked, name)
	r.Unlock()

	node := s.Nodes[0]
	_, err := r.Client.DeregisterInstance(&sd.DeregisterInstanceInput{
		InstanceId: aws.String(node.Id),
		ServiceId:  aws.String(r.serviceIDs[s.Name]),
	})

	return err
}

func (r *cregistry) GetService(name string) ([]*registry.Service, error) {
	name = sanitizeServiceName(name)

	var services []*registry.Service
	rsp, err := r.Client.DiscoverInstances(&sd.DiscoverInstancesInput{
		NamespaceName: aws.String(r.Domain),
		ServiceName:   aws.String(name),
	})

	if err != nil {
		return services, err
	}

	for _, instance := range rsp.Instances {
		// just in case
		if aws.StringValue(instance.ServiceName) != name || aws.StringValue(instance.HealthStatus) == "UNHEALTHY" {
			continue
		}

		svc := &registry.Service{
			Name:      aws.StringValue(instance.ServiceName),
			Version:   decodeVersion(aws.StringValue(instance.Attributes[attrVersion])),
			Endpoints: decodeEndpoints(aws.StringValue(instance.Attributes[attrEndpoints])),
		}
		services = append(services, svc)

		port, err := strconv.Atoi(aws.StringValue(instance.Attributes[attrPort]))
		if err != nil {
			// unusable
			continue
		}
		svc.Nodes = append(svc.Nodes, &registry.Node{
			Id:       aws.StringValue(instance.InstanceId),
			Address:  aws.StringValue(instance.Attributes[attrIP]),
			Port:     port,
			Metadata: decodeMetadata(aws.StringValue(instance.Attributes[attrMetadata])),
		})
	}

	return services, nil
}

func (r *cregistry) ListServices() ([]*registry.Service, error) {
	var services []*registry.Service
	err := r.Client.ListServicesPages(&sd.ListServicesInput{},
		func(page *sd.ListServicesOutput, lastPage bool) bool {
			for _, svc := range page.Services {
				services = append(services, &registry.Service{Name: aws.StringValue(svc.Name)})
			}
			return true
		})

	if err != nil {
		return nil, err
	}

	return services, nil
}

func (r *cregistry) Watch(opts ...registry.WatchOption) (registry.Watcher, error) {
	return newWatcher(r, opts...)
}

func (r *cregistry) String() string {
	return "cloudmap"
}

func newRegistry(opts ...registry.Option) registry.Registry {
	r := &cregistry{
		opts:        registry.Options{},
		register:    make(map[string]uint64),
		lastChecked: make(map[string]time.Time),
		serviceIDs:  make(map[string]string),
	}
	configure(r, opts...)
	return r
}

// NewRegistry creates an instance of the AWS CloudMap Registry
func NewRegistry(opts ...registry.Option) registry.Registry {
	return newRegistry(opts...)
}

func (r *cregistry) findServiceID(name string) (string, bool) {
	var id string
	err := r.Client.ListServicesPages(&sd.ListServicesInput{},
		func(page *sd.ListServicesOutput, lastPage bool) bool {
			for _, svc := range page.Services {
				if aws.StringValue(svc.Name) == name {
					id = aws.StringValue(svc.Id)
					r.serviceIDs[name] = id
					return false
				}
			}
			return true
		})
	if err != nil {
		return "", false
	}

	return id, len(id) > 0
}

// Major hackery ahead
func sanitizeServiceName(name string) string {
	// TODO setup constant to iterate through
	if strings.HasPrefix(name, "topic:") {
		return strings.Replace(name, "topic:", "topic-", 1)
	}

	if strings.HasPrefix(name, "broker-") {
		h, err := hash.Hash(name, nil)
		if err != nil {
			return name
		}

		return "broker-" + string(h)
	}

	return name
}

