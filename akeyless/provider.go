package akeyless

import (
	"context"
	"errors"
	"fmt"
	"github.com/akeylesslabs/akeyless-go-cloud-id/cloudprovider/aws"
	"github.com/akeylesslabs/akeyless-go-cloud-id/cloudprovider/azure"
	"github.com/akeylesslabs/akeyless-go/v2"
	"github.com/akeylesslabs/terraform-provider-akeyless/akeyless/common"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"os"
)

// default: public API Gateway
const publicApi = "https://api.akeyless.io"

var apiKeyLogin []interface{}
var emailLogin []interface{}
var awsIAMLogin []interface{}
var azureADLogin []interface{}

// Provider returns Akeyless Terraform provider
func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"api_gateway_address": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     publicApi,
				Description: "Origin URL of the API Gateway server. This is a URL with a scheme, a hostname and a port.",
			},
			"api_key_login": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "A configuration block, described below, that attempts to authenticate using API-Key.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"access_id": {
							Type:        schema.TypeString,
							Required:    true,
							DefaultFunc: schema.EnvDefaultFunc("AKEYLESS_ACCESS_ID", nil),
						},
						"access_key": {
							Type:        schema.TypeString,
							Required:    true,
							Sensitive:   true,
							DefaultFunc: schema.EnvDefaultFunc("AKEYLESS_ACCESS_KEY", nil),
						},
					},
				},
			},
			"aws_iam_login": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "A configuration block, described below, that attempts to authenticate using AWS-IAM authentication credentials.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"access_id": {
							Type:     schema.TypeString,
							Required: true,
						},
					},
				},
			},
			"azure_ad_login": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "A configuration block, described below, that attempts to authenticate using Azure Active Directory authentication.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"access_id": {
							Type:     schema.TypeString,
							Required: true,
						},
					},
				},
			},
			"email_login": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "A configuration block, described below, that attempts to authenticate using email and password.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"admin_email": {
							Type:     schema.TypeString,
							Required: true,
						},
						"admin_password": {
							Type:     schema.TypeString,
							Required: true,
						},
					},
				},
			},
		},
		ConfigureFunc: configureProvider,
		ResourcesMap: map[string]*schema.Resource{
			"akeyless_static_secret": resourceStaticSecret(),
			"akeyless_auth_method":   resourceAuthMethod(),
			"akeyless_role":          resourceRole(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"akeyless_static_secret":  dataSourceStaticSecret(),
			"akeyless_secret":         dataSourceSecret(),
			"akeyless_auth_method":    dataSourceAuthMethod(),
			"akeyless_dynamic_secret": dataSourceDynamicSecret(),
			"akeyless_role":           dataSourceRole(),
		},
	}
}

func configureProvider(d *schema.ResourceData) (interface{}, error) {
	apiGwAddress := d.Get("api_gateway_address").(string)

	err := inputValidation(d)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	client := akeyless.NewAPIClient(&akeyless.Configuration{
		Servers: []akeyless.ServerConfiguration{
			{
				URL: apiGwAddress,
			},
		},
	}).V2Api

	authBody := akeyless.NewAuthWithDefaults()
	err = setAuthBody(authBody)
	if err != nil {
		return "", err
	}

	var apiErr akeyless.GenericOpenAPIError

	authOut, _, err := client.Auth(ctx).Body(*authBody).Execute()
	if err != nil {
		if errors.As(err, &apiErr) {
			return "", fmt.Errorf("authentication failed: %v", string(apiErr.Body()))
		}
		return "", fmt.Errorf("authentication failed: %v", err)
	}
	token := authOut.GetToken()

	return providerMeta{client, &token}, nil
}

func setAuthBody(authBody *akeyless.Auth) error {
	if apiKeyLogin != nil && len(apiKeyLogin) == 1 {
		login, ok := apiKeyLogin[0].(map[string]interface{})
		if ok {
			accessID := login["access_id"].(string)
			accessKey := login["access_key"].(string)
			authBody.AccessId = akeyless.PtrString(accessID)
			authBody.AccessKey = akeyless.PtrString(accessKey)
			authBody.AccessType = akeyless.PtrString(common.ApiKey)

			return nil
		}
	}

	if os.Getenv("AKEYLESS_ACCESS_ID") != "" && os.Getenv("AKEYLESS_ACCESS_KEY") != "" {
		authBody.AccessId = akeyless.PtrString(os.Getenv("AKEYLESS_ACCESS_ID"))
		authBody.AccessKey = akeyless.PtrString(os.Getenv("AKEYLESS_ACCESS_KEY"))
		authBody.AccessType = akeyless.PtrString(common.ApiKey)
		return nil
	}

	if emailLogin != nil && len(emailLogin) == 1 {
		login := emailLogin[0].(map[string]interface{})
		adminEmail := login["admin_email"].(string)
		adminPassword := login["admin_password"].(string)
		authBody.AdminEmail = akeyless.PtrString(adminEmail)
		authBody.AdminPassword = akeyless.PtrString(adminPassword)
		authBody.AccessType = akeyless.PtrString(common.Password)
	} else if awsIAMLogin != nil && len(awsIAMLogin) == 1 {
		login := awsIAMLogin[0].(map[string]interface{})
		accessID := login["access_id"].(string)
		authBody.AccessId = akeyless.PtrString(accessID)
		cloudId, err := aws.GetCloudId()
		if err != nil {
			return fmt.Errorf("require Cloud ID: %v", err.Error())
		}
		authBody.CloudId = akeyless.PtrString(cloudId)
		authBody.AccessType = akeyless.PtrString(common.AwsIAM)
	} else if azureADLogin != nil && len(azureADLogin) == 1 {
		login := azureADLogin[0].(map[string]interface{})
		accessID := login["access_id"].(string)
		authBody.AccessId = akeyless.PtrString(accessID)
		cloudId, err := azure.GetCloudId("")
		if err != nil {
			return fmt.Errorf("require Cloud ID: %v", err.Error())
		}
		authBody.CloudId = akeyless.PtrString(cloudId)
		authBody.AccessType = akeyless.PtrString(common.AzureAD)
	} else {
		return fmt.Errorf("please support login method: api_key_login/password_login/aws_iam_login/azure_ad_login")
	}

	return nil
}

type providerMeta struct {
	client *akeyless.V2ApiService
	token  *string
}

func inputValidation(d *schema.ResourceData) error {
	apiKeyLogin = d.Get("api_key_login").([]interface{})
	if len(apiKeyLogin) > 1 {
		return fmt.Errorf("api_key_login block may appear only once")
	}
	emailLogin = d.Get("email_login").([]interface{})
	if len(emailLogin) > 1 {
		return fmt.Errorf("emailLogin block may appear only once")
	}
	awsIAMLogin = d.Get("aws_iam_login").([]interface{})
	if len(awsIAMLogin) > 1 {
		return fmt.Errorf("aws_iam_login block may appear only once")
	}
	azureADLogin = d.Get("azure_ad_login").([]interface{})
	if len(azureADLogin) > 1 {
		return fmt.Errorf("azure_ad_login block may appear only once")
	}
	return nil
}
