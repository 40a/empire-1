package cloudformation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/remind101/empire/pkg/bytesize"
	"github.com/remind101/empire/scheduler"
)

const (
	// For HTTP/HTTPS/TCP services, we allocate an ELB and map it's instance port to
	// the container port. This is the port that processes within the container
	// should bind to. Tihs value is also exposed to the container through the PORT
	// environment variable.
	ContainerPort = 8080

	schemeInternal = "internal"
	schemeExternal = "internet-facing"

	defaultConnectionDrainingTimeout int64 = 30
	defaultCNAMETTL                        = 60
)

// This implements the Template interface to create a suitable CloudFormation
// template for an Empire app.
type EmpireTemplate struct {
	// The ECS cluster to run the services in.
	Cluster string

	// The hosted zone to add CNAME's to.
	HostedZone *route53.HostedZone

	// The ID of the security group to assign to internal load balancers.
	InternalSecurityGroupID string

	// The ID of the security group to assign to external load balancers.
	ExternalSecurityGroupID string

	// The Subnet IDs to assign when creating internal load balancers.
	InternalSubnetIDs []string

	// The Subnet IDs to assign when creating external load balancers.
	ExternalSubnetIDs []string

	// The name of the ECS Service IAM role.
	ServiceRole string

	// The ARN of the SNS topic to provision instance ports.
	CustomResourcesTopic string

	LogConfiguration *ecs.LogConfiguration
}

// Execute builds the template, and writes it to w.
func (t *EmpireTemplate) Execute(w io.Writer, data interface{}) error {
	v, err := t.Build(data.(*scheduler.App))
	if err != nil {
		return err
	}

	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	_, err = io.Copy(w, bytes.NewReader(raw))
	return err
}

// Build builds a Go representation of a CloudFormation template for the app.
func (t *EmpireTemplate) Build(app *scheduler.App) (interface{}, error) {
	parameters := map[string]interface{}{}
	resources := map[string]interface{}{}
	outputs := map[string]interface{}{}

	for _, p := range app.Processes {
		cd := t.ContainerDefinition(p)

		// CloudFormation only allows alphanumeric resource names, so we
		// have to normalize it.
		r := regexp.MustCompile("[^a-zA-Z0-9]")
		key := r.ReplaceAllString(p.Type, "")

		portMappings := []map[string]interface{}{}

		loadBalancers := []map[string]interface{}{}
		if p.Exposure != nil {
			scheme := schemeInternal
			sg := t.InternalSecurityGroupID
			subnets := t.InternalSubnetIDs

			if p.Exposure.External {
				scheme = schemeExternal
				sg = t.ExternalSecurityGroupID
				subnets = t.ExternalSubnetIDs
			}

			instancePort := fmt.Sprintf("%s%dInstancePort", key, ContainerPort)
			resources[instancePort] = map[string]interface{}{
				"Type":    "Custom::InstancePort",
				"Version": "1.0",
				"Properties": map[string]interface{}{
					"ServiceToken": t.CustomResourcesTopic,
				},
			}

			listeners := []map[string]interface{}{
				map[string]interface{}{
					"LoadBalancerPort": 80,
					"Protocol":         "http",
					"InstancePort": map[string][]string{
						"Fn::GetAtt": []string{
							instancePort,
							"InstancePort",
						},
					},
					"InstanceProtocol": "http",
				},
			}

			if e, ok := p.Exposure.Type.(*scheduler.HTTPSExposure); ok {
				listeners = append(listeners, map[string]interface{}{
					"LoadBalancerPort": 80,
					"Protocol":         "http",
					"InstancePort": map[string][]string{
						"Fn::GetAtt": []string{
							instancePort,
							"InstancePort",
						},
					},
					"SSLCertificateId": e.Cert,
					"InstanceProtocol": "http",
				})
			}

			portMappings = append(portMappings, map[string]interface{}{
				"ContainerPort": ContainerPort,
				"HostPort": map[string][]string{
					"Fn::GetAtt": []string{
						instancePort,
						"InstancePort",
					},
				},
			})
			cd.Environment = append(cd.Environment, &ecs.KeyValuePair{
				Name:  aws.String("PORT"),
				Value: aws.String(fmt.Sprintf("%d", ContainerPort)),
			})

			loadBalancer := fmt.Sprintf("%sLoadBalancer", key)
			loadBalancers = append(loadBalancers, map[string]interface{}{
				"ContainerName": p.Type,
				"ContainerPort": ContainerPort,
				"LoadBalancerName": map[string]string{
					"Ref": loadBalancer,
				},
			})
			resources[loadBalancer] = map[string]interface{}{
				"Type": "AWS::ElasticLoadBalancing::LoadBalancer",
				"Properties": map[string]interface{}{
					"Scheme":         scheme,
					"SecurityGroups": []string{sg},
					"Subnets":        subnets,
					"Listeners":      listeners,
					"CrossZone":      true,
					"Tags": []map[string]string{
						map[string]string{
							"Key":   "empire.app.process",
							"Value": p.Type,
						},
					},
					"ConnectionDrainingPolicy": map[string]interface{}{
						"Enabled": true,
						"Timeout": defaultConnectionDrainingTimeout,
					},
				},
			}

			if p.Type == "web" {
				resources["CNAME"] = map[string]interface{}{
					"Type": "AWS::Route53::RecordSet",
					"Properties": map[string]interface{}{
						"HostedZoneId": *t.HostedZone.Id,
						"Name":         fmt.Sprintf("%s.%s", app.Name, *t.HostedZone.Name),
						"Type":         "CNAME",
						"TTL":          defaultCNAMETTL,
						"ResourceRecords": []map[string]string{
							map[string]string{
								"Ref": loadBalancer,
							},
						},
					},
				}
			}
		}

		taskDefinition := fmt.Sprintf("%sTaskDefinition", key)
		containerDefinition := map[string]interface{}{
			"Name":         *cd.Name,
			"Command":      cd.Command,
			"Cpu":          *cd.Cpu,
			"Image":        *cd.Image,
			"Essential":    *cd.Essential,
			"Memory":       *cd.Memory,
			"Environment":  cd.Environment,
			"PortMappings": portMappings,
			"DockerLabels": cd.DockerLabels,
			"Ulimits":      cd.Ulimits,
		}
		if cd.LogConfiguration != nil {
			containerDefinition["LogConfiguration"] = cd.LogConfiguration
		}
		resources[taskDefinition] = map[string]interface{}{
			"Type": "AWS::ECS::TaskDefinition",
			"Properties": map[string]interface{}{
				"ContainerDefinitions": []interface{}{
					containerDefinition,
				},
				"Volumes": []interface{}{},
			},
		}

		service := fmt.Sprintf("%s", key)
		serviceProperties := map[string]interface{}{
			"Cluster":       t.Cluster,
			"DesiredCount":  p.Instances,
			"LoadBalancers": loadBalancers,
			"TaskDefinition": map[string]string{
				"Ref": taskDefinition,
			},
		}
		if len(loadBalancers) > 0 {
			serviceProperties["Role"] = t.ServiceRole
		}
		resources[service] = map[string]interface{}{
			"Type": "AWS::ECS::Service",
			"Metadata": &serviceMetadata{
				Name: p.Type,
			},
			"Properties": serviceProperties,
		}

	}

	return map[string]interface{}{
		"Parameters": parameters,
		"Resources":  resources,
		"Outputs":    outputs,
	}, nil
}

// ContainerDefinition generates an ECS ContainerDefinition for a process.
func (t *EmpireTemplate) ContainerDefinition(p *scheduler.Process) *ecs.ContainerDefinition {
	command := []*string{}
	for _, s := range p.Command {
		ss := s
		command = append(command, &ss)
	}

	environment := []*ecs.KeyValuePair{}
	for k, v := range p.Env {
		environment = append(environment, &ecs.KeyValuePair{
			Name:  aws.String(k),
			Value: aws.String(v),
		})
	}

	labels := make(map[string]*string)
	for k, v := range p.Labels {
		labels[k] = aws.String(v)
	}

	ulimits := []*ecs.Ulimit{}
	if p.Nproc != 0 {
		ulimits = []*ecs.Ulimit{
			&ecs.Ulimit{
				Name:      aws.String("nproc"),
				SoftLimit: aws.Int64(int64(p.Nproc)),
				HardLimit: aws.Int64(int64(p.Nproc)),
			},
		}
	}

	return &ecs.ContainerDefinition{
		Name:             aws.String(p.Type),
		Cpu:              aws.Int64(int64(p.CPUShares)),
		Command:          command,
		Image:            aws.String(p.Image.String()),
		Essential:        aws.Bool(true),
		Memory:           aws.Int64(int64(p.MemoryLimit / bytesize.MB)),
		Environment:      environment,
		LogConfiguration: t.LogConfiguration,
		DockerLabels:     labels,
		Ulimits:          ulimits,
	}
}

// HostedZone returns the HostedZone for the ZoneID.
func HostedZone(config client.ConfigProvider, hostedZoneID string) (*route53.HostedZone, error) {
	r := route53.New(config)
	zid := fixHostedZoneIDPrefix(hostedZoneID)
	out, err := r.GetHostedZone(&route53.GetHostedZoneInput{Id: zid})
	if err != nil {
		return nil, err
	}

	return out.HostedZone, nil
}

func fixHostedZoneIDPrefix(zoneID string) *string {
	prefix := "/hostedzone/"
	s := zoneID
	if ok := strings.HasPrefix(zoneID, prefix); !ok {
		s = strings.Join([]string{prefix, zoneID}, "")
	}
	return &s
}