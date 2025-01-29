package service

import (
	"context"
	"time"

	"github.com/awslabs/ssosync/internal"
	"github.com/awslabs/ssosync/internal/aws"
	"github.com/awslabs/ssosync/internal/config"
	"github.com/awslabs/ssosync/internal/google"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/tailscale/setec/client/setec"

	aws_sdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/identitystore"
)

var (
	// setec keys for configuration items
	scimEndpointSecret      = "prod/ssosync/scim-endpoint"
	scimTokenSecret         = "prod/ssosync/scim-token"
	googleCredentialsSecret = "prod/ssosync/google-credentials"
	googleAdminSecret       = "prod/ssosync/google-admin"
	identityStoreIDSecret   = "prod/ssosync/identity-store-id"

	// optional user / group filtering to scope sync
	matchUsers  = ""
	matchGroups = ""
)

// NewConfigFromSetec fetches the config.Config credentials from the setec server at the given URL.
func NewConfigFromSetec(ctx context.Context, setecURL string) (*config.Config, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client: setec.Client{Server: setecURL},
		Secrets: []string{
			scimEndpointSecret,
			scimTokenSecret,
			googleCredentialsSecret,
			googleAdminSecret,
			identityStoreIDSecret,
		},
	})
	if err != nil {
		return nil, err
	}

	return &config.Config{
		SCIMEndpoint:      st.Secret(scimEndpointSecret).GetString(),
		SCIMAccessToken:   st.Secret(scimTokenSecret).GetString(),
		GoogleCredentials: st.Secret(googleCredentialsSecret).GetString(),
		GoogleAdmin:       st.Secret(googleAdminSecret).GetString(),
		IdentityStoreID:   st.Secret(identityStoreIDSecret).GetString(),
		UserMatch:         matchUsers,
		GroupMatch:        matchGroups,
	}, nil
}

// RunSync performs a single group sync operation using the provided configuration.
func RunSync(ctx context.Context, config *config.Config) error {
	retryClient := retryablehttp.NewClient()
	httpClient := retryClient.StandardClient()

	googleClient, err := google.NewClient(ctx, config.GoogleAdmin, []byte(config.GoogleCredentials))
	if err != nil {
		return err
	}

	awsScimClient, err := aws.NewClient(
		httpClient,
		&aws.Config{
			Endpoint: config.SCIMEndpoint,
			Token:    config.SCIMAccessToken,
		},
	)
	if err != nil {
		return err
	}

	sess, err := session.NewSession(&aws_sdk.Config{
		Region: &config.Region,
	})
	if err != nil {
		return err
	}

	identityStoreClient := identitystore.New(sess)
	_, err = identityStoreClient.ListGroups(&identitystore.ListGroupsInput{IdentityStoreId: &config.IdentityStoreID})
	if err != nil {
		return err
	}

	syncClient := internal.New(config, awsScimClient, googleClient, identityStoreClient)
	return syncClient.SyncGroupsUsers(config.GroupMatch, config.UserMatch)
}
