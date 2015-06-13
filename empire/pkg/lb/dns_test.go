package lb

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/remind101/empire/empire/pkg/awsutil"
	"github.com/remind101/empire/empire/pkg/httpmock"
)

func TestRoute53_CNAME(t *testing.T) {
	h := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /2013-04-01/hostedzone/FAKEZONE", nil,
		200, `<?xml version="1.0"?>
<GetHostedZoneResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/">
	<HostedZone>
		<Id>/hostedzone/FAKEZONE</Id>
		<Name>empire.</Name>
		<CallerReference>FakeReference</CallerReference>
		<Config>
			<Comment>Fake hosted zone comment.</Comment>
			<PrivateZone>true</PrivateZone>
		</Config>
		<ResourceRecordSetCount>2</ResourceRecordSetCount>
	</HostedZone>
	<VPCs>
		<VPC>
			<VPCRegion>us-east-1</VPCRegion>
			<VPCId>vpc-0d9ea668</VPCId>
		</VPC>
	</VPCs>
</GetHostedZoneResponse>`,
	)).Add(httpmock.PathHandler(t,
		`POST /2013-04-01/hostedzone/FAKEZONE/rrset`, aws.String("{}"),
		200, ``,
	))

	n, s := newTestRoute53Nameserver(h, "/hostedzone/FAKEZONE")
	defer s.Close()

	if err := n.CreateCNAME("acme-inc", "123456789.us-east-1.elb.amazonaws.com"); err != nil {
		t.Fatal(err)
	}
}

func TestRoute53_DeleteCNAME(t *testing.T) {
	h := awsutil.NewHandler(t, []awsutil.Cycle{
		{
			Request: awsutil.Request{
				RequestURI: "/2013-04-01/hostedzone/FAKEZONE",
				Body:       ``,
			},
			Response: awsutil.Response{
				StatusCode: 200,
				Body: `<?xml version="1.0"?>
<GetHostedZoneResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/">
	<HostedZone>
		<Id>/hostedzone/FAKEZONE</Id>
		<Name>empire.</Name>
		<CallerReference>FakeReference</CallerReference>
		<Config>
			<Comment>Fake hosted zone comment.</Comment>
			<PrivateZone>true</PrivateZone>
		</Config>
		<ResourceRecordSetCount>2</ResourceRecordSetCount>
	</HostedZone>
	<VPCs>
		<VPC>
			<VPCRegion>us-east-1</VPCRegion>
			<VPCId>vpc-0d9ea668</VPCId>
		</VPC>
	</VPCs>
</GetHostedZoneResponse>`,
			},
		},
		{
			Request: awsutil.Request{
				RequestURI: `/2013-04-01/hostedzone/FAKEZONE/rrset`,
				Body:       `ignore`,
			},
			Response: awsutil.Response{
				StatusCode: 200,
				Body:       ``,
			},
		},
	})

	n, s := newTestRoute53Nameserver(h, "/hostedzone/FAKEZONE")
	defer s.Close()

	if err := n.DeleteCNAME("acme-inc", "123456789.us-east-1.elb.amazonaws.com"); err != nil {
		t.Fatal(err)
	}
}

func TestRoute53_zone(t *testing.T) {
	h := awsutil.NewHandler(t, []awsutil.Cycle{
		{
			Request: awsutil.Request{
				RequestURI: "/2013-04-01/hostedzone/FAKEZONE",
				Body:       ``,
			},
			Response: awsutil.Response{
				StatusCode: 200,
				Body: `<?xml version="1.0"?>
<GetHostedZoneResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/">
	<HostedZone>
		<Id>/hostedzone/FAKEZONE</Id>
		<Name>empire.</Name>
		<CallerReference>FakeReference</CallerReference>
		<Config>
			<Comment>Fake hosted zone comment.</Comment>
			<PrivateZone>true</PrivateZone>
		</Config>
		<ResourceRecordSetCount>2</ResourceRecordSetCount>
	</HostedZone>
	<VPCs>
		<VPC>
			<VPCRegion>us-east-1</VPCRegion>
			<VPCId>vpc-0d9ea668</VPCId>
		</VPC>
	</VPCs>
</GetHostedZoneResponse>`,
			},
		},
		{
			Request: awsutil.Request{
				RequestURI: "/2013-04-01/hostedzone/FAKEZONE",
				Body:       ``,
			},
			Response: awsutil.Response{
				StatusCode: 200,
				Body: `<?xml version="1.0"?>
<GetHostedZoneResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/">
	<HostedZone>
		<Id>/hostedzone/FAKEZONE</Id>
		<Name>empire.</Name>
		<CallerReference>FakeReference</CallerReference>
		<Config>
			<Comment>Fake hosted zone comment.</Comment>
			<PrivateZone>true</PrivateZone>
		</Config>
		<ResourceRecordSetCount>2</ResourceRecordSetCount>
	</HostedZone>
	<VPCs>
		<VPC>
			<VPCRegion>us-east-1</VPCRegion>
			<VPCId>vpc-0d9ea668</VPCId>
		</VPC>
	</VPCs>
</GetHostedZoneResponse>`,
			},
		},
	})

	// Test both a full path to a zoneID and just the zoneID itself
	// Route53Nameserver.zone() should be able to handle both.
	zoneIDs := []string{"/hostedzone/FAKEZONE", "FAKEZONE"}
	for _, zid := range zoneIDs {
		n, s := newTestRoute53Nameserver(h, zid)
		defer s.Close()

		zone, err := n.zone()
		if err != nil {
			t.Fatal(err)
		}

		if *zone.ID != "/hostedzone/FAKEZONE" {
			t.Fatalf("Got wrong zone ID: %s\n", *zone.ID)
		}
	}
}

func newTestRoute53Nameserver(h http.Handler, zoneID string) (*Route53Nameserver, *httptest.Server) {
	s := httptest.NewServer(h)

	n := NewRoute53Nameserver(
		aws.DefaultConfig.Merge(&aws.Config{
			Credentials: credentials.NewStaticCredentials(" ", " ", " "),
			Endpoint:    s.URL,
			Region:      "localhost",
			LogLevel:    0,
		}),
	)
	n.ZoneID = zoneID

	return n, s
}
