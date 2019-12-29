package provision

import (
	"encoding/base64"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// AWSProvisioner provision a VM on aws
type AWSProvisioner struct {
	client *ec2.EC2
}

type ami struct {
	ID           string
	CreationDate time.Time
}

// NewAWSProvisioner with an accessKey and secretKey
func NewAWSProvisioner(accessKey, secretKey, region string) (*AWSProvisioner, error) {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKey, secretKey, ""),
	})

	if err != nil {
		return nil, err
	}

	// Create EC2 service client
	svc := ec2.New(sess)

	return &AWSProvisioner{
		client: svc,
	}, nil
}

// Provision is creating an instance on the defined region
// It tries to use the default VPC or re-create it using the CreateDefaultVpc function
func (p *AWSProvisioner) Provision(host BasicHost) (*ProvisionedHost, error) {
	ami, err := p.findAMI(host.OS)
	if err != nil {
		return nil, err
	}

	runResult, err := p.client.RunInstances(&ec2.RunInstancesInput{
		ImageId:      aws.String(ami),
		InstanceType: aws.String(host.Plan),
		MinCount:     aws.Int64(1),
		MaxCount:     aws.Int64(1),
		UserData:     aws.String(base64.StdEncoding.EncodeToString([]byte(host.UserData))),
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/sdh"),
				Ebs: &ec2.EbsBlockDevice{
					VolumeSize: aws.Int64(20),
				},
			},
		},
		TagSpecifications: []*ec2.TagSpecification{
			{
				ResourceType: aws.String("instance"),
				Tags: []*ec2.Tag{
					{
						Key:   aws.String("name"),
						Value: aws.String(host.Name),
					},
				},
			},
		},
	})

	if err != nil {
		return nil, err
	}

	return reservationToPrivionedHost(runResult), nil
}

// Status returns the status of the aws instance
func (p *AWSProvisioner) Status(id string) (*ProvisionedHost, error) {
	input := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			aws.String(id),
		},
	}

	describeResult, err := p.client.DescribeInstances(input)
	if err != nil {
		return nil, err
	}

	result := describeResult.Reservations[0]

	return reservationToPrivionedHost(result), nil
}

// Delete deletes the provisionned instance by ID
func (p *AWSProvisioner) Delete(id string) error {
	input := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(id),
		},
	}

	_, err := p.client.TerminateInstances(input)
	return err
}

func (p *AWSProvisioner) findAMI(name string) (string, error) {
	input := &ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String("name"),
				Values: []*string{aws.String(name)},
			},
			&ec2.Filter{
				Name:   aws.String("is-public"),
				Values: []*string{aws.String("true")},
			},
			&ec2.Filter{
				Name:   aws.String("architecture"),
				Values: []*string{aws.String("x86_64")},
			},
			&ec2.Filter{
				Name:   aws.String("state"),
				Values: []*string{aws.String("available")},
			},
		},
	}

	describeResult, err := p.client.DescribeImages(input)
	if err != nil {
		return "", err
	}

	images := []ami{}
	for _, image := range describeResult.Images {
		parsed, err := time.Parse(time.RFC3339, *image.CreationDate)
		if err != nil {
			break
		}
		images = append(images, ami{
			ID:           *image.ImageId,
			CreationDate: parsed,
		})
	}

	// Ensure we choose the lastest ami:
	sort.Slice(images, func(i, j int) bool {
		return images[i].CreationDate.After(images[j].CreationDate)
	})

	return images[0].ID, nil
}

func reservationToPrivionedHost(reservation *ec2.Reservation) *ProvisionedHost {
	instance := reservation.Instances[0]

	var ip string
	if instance.PublicIpAddress != nil {
		ip = *instance.PublicIpAddress
	}

	state := *instance.State.Name
	if state == "running" {
		state = ActiveStatus
	}

	return &ProvisionedHost{
		ID:     *instance.InstanceId,
		IP:     ip,
		Status: state,
	}
}
