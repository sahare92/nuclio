/*
Copyright 2017 The Nuclio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package test

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/nuclio/nuclio/pkg/common"
	"github.com/nuclio/nuclio/pkg/functionconfig"
	"github.com/nuclio/nuclio/pkg/platform"
	"github.com/nuclio/nuclio/pkg/platform/kube"
	nuclioio "github.com/nuclio/nuclio/pkg/platform/kube/apis/nuclio.io/v1beta1"
	"github.com/nuclio/nuclio/pkg/platformconfig"

	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeployFunctionTestSuite struct {
	KubeTestSuite
}

func (suite *DeployFunctionTestSuite) SetupSuite() {
	suite.KubeTestSuite.SetupSuite()

	// start controller in background
	go suite.Controller.Start() // nolint: errcheck
}

func (suite *DeployFunctionTestSuite) TestStaleResourceVersion() {
	var resourceVersion string

	createFunctionOptions := suite.compileCreateFunctionOptions("resource-schema")

	afterFirstDeploy := func(deployResult *platform.CreateFunctionResult) bool {

		// get the function
		functions, err := suite.Platform.GetFunctions(&platform.GetFunctionsOptions{
			Name:      createFunctionOptions.FunctionConfig.Meta.Name,
			Namespace: createFunctionOptions.FunctionConfig.Meta.Namespace,
		})
		suite.Require().NoError(err)
		function := functions[0]

		// save resource version
		resourceVersion = function.GetConfig().Meta.ResourceVersion
		suite.Require().NotEmpty(resourceVersion)

		// ensure using newest resource version on second deploy
		createFunctionOptions.FunctionConfig.Meta.ResourceVersion = resourceVersion

		// change source code
		createFunctionOptions.FunctionConfig.Spec.Build.FunctionSourceCode = base64.StdEncoding.EncodeToString([]byte(`
def handler(context, event):
  return "darn it!"
`))
		return true
	}

	afterSecondDeploy := func(deployResult *platform.CreateFunctionResult) bool {

		// get the function
		functions, err := suite.Platform.GetFunctions(&platform.GetFunctionsOptions{
			Name:      createFunctionOptions.FunctionConfig.Meta.Name,
			Namespace: createFunctionOptions.FunctionConfig.Meta.Namespace,
		})
		suite.Require().NoError(err, "Failed to get functions")

		function := functions[0]
		suite.Require().NotEqual(resourceVersion,
			function.GetConfig().Meta.ResourceVersion,
			"Resource version should be changed between deployments")

		// we expect a failure due to a stale resource version
		suite.DeployFunctionExpectError(createFunctionOptions, func(deployResult *platform.CreateFunctionResult) bool {
			suite.Require().Nil(deployResult, "Deployment results is nil when creation failed")
			return true
		})

		return true
	}

	suite.DeployFunctionAndRedeploy(createFunctionOptions, afterFirstDeploy, afterSecondDeploy)
}

func (suite *DeployFunctionTestSuite) TestAugmentedConfig() {
	runAsUserID := int64(1000)
	runAsGroupID := int64(2000)
	functionAvatar := "demo-avatar"
	functionLabels := map[string]string{
		"my-function": "is-labeled",
	}
	suite.PlatformConfiguration.FunctionAugmentedConfigs = []platformconfig.LabelSelectorAndConfig{
		{
			LabelSelector: metav1.LabelSelector{
				MatchLabels: functionLabels,
			},
			FunctionConfig: functionconfig.Config{
				Spec: functionconfig.Spec{
					Avatar: functionAvatar,
				},
			},
			Kubernetes: platformconfig.Kubernetes{
				Deployment: &appsv1.Deployment{
					Spec: appsv1.DeploymentSpec{
						Template: v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								SecurityContext: &v1.PodSecurityContext{
									RunAsUser:  &runAsUserID,
									RunAsGroup: &runAsGroupID,
								},
							},
						},
					},
				},
			},
		},
	}
	functionName := "augmented-config"
	createFunctionOptions := suite.compileCreateFunctionOptions(functionName)
	createFunctionOptions.FunctionConfig.Meta.Labels = functionLabels
	suite.DeployFunction(createFunctionOptions, func(deployResult *platform.CreateFunctionResult) bool {
		deploymentInstance := &appsv1.Deployment{}
		functionInstance := &nuclioio.NuclioFunction{}
		suite.getResourceAndUnmarshal("nucliofunction",
			functionName,
			functionInstance)
		suite.getResourceAndUnmarshal("deployment",
			kube.DeploymentNameFromFunctionName(functionName),
			deploymentInstance)

		// ensure function spec was enriched
		suite.Require().Equal(functionAvatar, functionInstance.Spec.Avatar)

		// ensure function deployment was enriched
		suite.Require().NotNil(deploymentInstance.Spec.Template.Spec.SecurityContext.RunAsUser)
		suite.Require().NotNil(deploymentInstance.Spec.Template.Spec.SecurityContext.RunAsGroup)
		suite.Require().Equal(runAsUserID, *deploymentInstance.Spec.Template.Spec.SecurityContext.RunAsUser)
		suite.Require().Equal(runAsGroupID, *deploymentInstance.Spec.Template.Spec.SecurityContext.RunAsGroup)
		return true
	})
}

func (suite *DeployFunctionTestSuite) TestMinMaxReplicas() {
	functionName := "min-max-replicas"
	two := 2
	three := 3
	createFunctionOptions := suite.compileCreateFunctionOptions(functionName)
	createFunctionOptions.FunctionConfig.Spec.MinReplicas = &two
	createFunctionOptions.FunctionConfig.Spec.MaxReplicas = &three
	suite.DeployFunction(createFunctionOptions, func(deployResult *platform.CreateFunctionResult) bool {
		hpaInstance := &autoscalingv1.HorizontalPodAutoscaler{}
		suite.getResourceAndUnmarshal("hpa", kube.HPANameFromFunctionName(functionName), hpaInstance)
		suite.Require().Equal(two, int(*hpaInstance.Spec.MinReplicas))
		suite.Require().Equal(three, int(hpaInstance.Spec.MaxReplicas))
		return true
	})
}

func (suite *DeployFunctionTestSuite) TestDefaultHTTPTrigger() {
	defaultTriggerFunctionName := "with-default-http-trigger"
	createDefaultTriggerFunctionOptions := suite.compileCreateFunctionOptions(defaultTriggerFunctionName)
	suite.DeployFunction(createDefaultTriggerFunctionOptions, func(deployResult *platform.CreateFunctionResult) bool {

		// ensure only 1 http trigger exists, always.
		suite.ensureTriggerAmount(defaultTriggerFunctionName, "http", 1)
		defaultHTTPTrigger := functionconfig.GetDefaultHTTPTrigger()
		return suite.verifyCreatedTrigger(defaultTriggerFunctionName, defaultHTTPTrigger)
	})

	customTriggerFunctionName := "custom-http-trigger"
	createCustomTriggerFunctionOptions := suite.compileCreateFunctionOptions(customTriggerFunctionName)
	customTrigger := functionconfig.Trigger{
		Kind:       "http",
		Name:       "custom-trigger",
		MaxWorkers: 3,
	}
	createCustomTriggerFunctionOptions.FunctionConfig.Spec.Triggers = map[string]functionconfig.Trigger{
		customTrigger.Name: customTrigger,
	}
	suite.DeployFunction(createCustomTriggerFunctionOptions, func(deployResult *platform.CreateFunctionResult) bool {

		// ensure only 1 http trigger exists, always.
		suite.ensureTriggerAmount(customTriggerFunctionName, "http", 1)
		return suite.verifyCreatedTrigger(customTriggerFunctionName, customTrigger)
	})
}

func (suite *DeployFunctionTestSuite) verifyCreatedTrigger(functionName string, trigger functionconfig.Trigger) bool {
	functionInstance := &nuclioio.NuclioFunction{}
	suite.getResourceAndUnmarshal("nucliofunction",
		functionName,
		functionInstance)

	// TODO: verify other parts of the trigger spec
	suite.Require().Equal(trigger.Name, functionInstance.Spec.Triggers[trigger.Name].Name)
	suite.Require().Equal(trigger.Kind, functionInstance.Spec.Triggers[trigger.Name].Kind)
	suite.Require().Equal(trigger.MaxWorkers, functionInstance.Spec.Triggers[trigger.Name].MaxWorkers)
	return true
}

func (suite *DeployFunctionTestSuite) ensureTriggerAmount(functionName, triggerKind string, amount int) {
	functionInstance := &nuclioio.NuclioFunction{}
	suite.getResourceAndUnmarshal("nucliofunction",
		functionName,
		functionInstance)

	functionHTTPTriggers := functionconfig.GetTriggersByKind(functionInstance.Spec.Triggers, triggerKind)
	suite.Require().Equal(amount, len(functionHTTPTriggers))
}

func (suite *DeployFunctionTestSuite) compileCreateFunctionOptions(
	functionName string) *platform.CreateFunctionOptions {

	createFunctionOptions := suite.KubeTestSuite.compileCreateFunctionOptions(functionName)
	createFunctionOptions.FunctionConfig.Spec.Handler = "main:handler"
	createFunctionOptions.FunctionConfig.Spec.Runtime = "python:3.6"
	createFunctionOptions.FunctionConfig.Spec.Build.FunctionSourceCode = base64.StdEncoding.EncodeToString([]byte(`
def handler(context, event):
  return "hello world"
`))
	return createFunctionOptions
}


type APIGatewayTestSuite struct {
	KubeTestSuite
}

func (suite *APIGatewayTestSuite) SetupSuite() {
	suite.KubeTestSuite.SetupSuite()

	// start controller in background
	go suite.Controller.Start() // nolint: errcheck
}

func (suite *APIGatewayTestSuite) TestCreate() {
	createFunctionOptions := suite.compileCreateFunctionOptions("test-api-gateway-func")

	functionSourceCode := base64.StdEncoding.EncodeToString([]byte(`def handler(context, event):
    return "expected response"
`))
	createFunctionOptions.FunctionConfig.Spec.Handler = "main:handler"
	createFunctionOptions.FunctionConfig.Spec.Runtime = "python:3.6"
	createFunctionOptions.FunctionConfig.Spec.Build.FunctionSourceCode = functionSourceCode

	// after the function is created, create the api gateway with it as its primary upstream
	afterFirstDeploy := func(deployResult *platform.CreateFunctionResult) bool {

		// get the function
		functions, err := suite.Platform.GetFunctions(&platform.GetFunctionsOptions{
			Name:      createFunctionOptions.FunctionConfig.Meta.Name,
			Namespace: createFunctionOptions.FunctionConfig.Meta.Namespace,
		})
		suite.Require().NoError(err)

		suite.Require().Lenf(functions, 1, "Unexpected amount of functions. actual: %s, expected: %s", len(functions), 1)

		function := functions[0]

		createAPIGatewayConfig := suite.compileCreateAPIGatewayOptions("test-api-gateway")
		createAPIGatewayConfig.APIGatewayConfig.Spec.Path = "/pathy"
		createAPIGatewayConfig.APIGatewayConfig.Spec.Upstreams = []platform.APIGatewayUpstreamSpec{
			{
				Kind: platform.APIGatewayUpstreamKindNuclioFunction,
				Nucliofunction: &platform.NuclioFunctionAPIGatewaySpec{
					Name: function.GetConfig().Meta.Name,
				},
			},
		}



		err = suite.Platform.CreateAPIGateway(createAPIGatewayConfig)
		suite.Require().NoError(err)

		defer suite.Platform.DeleteAPIGateway(&platform.DeleteAPIGatewayOptions{ // nolint: errcheck
			Meta: platform.APIGatewayMeta{
				Name: createAPIGatewayConfig.APIGatewayConfig.Meta.Name,
				Namespace: createAPIGatewayConfig.APIGatewayConfig.Meta.Namespace,
			},
		})

		// invoke the api-gateway URL to make sure it works (we get the expected function response)
		err = common.RetryUntilSuccessful(20 * time.Second, 1 * time.Second, func() bool {
			apiGatewayURL := fmt.Sprintf("http://%s%s",
				createAPIGatewayConfig.APIGatewayConfig.Spec.Host,
				createAPIGatewayConfig.APIGatewayConfig.Spec.Path)
			resp, err := http.Get(apiGatewayURL)
			if err != nil {
				suite.Logger.WarnWith("Failed while sending http /GET request to api gateway URL",
					"apiGatewayURL", apiGatewayURL,
					"err", err)
				return false
			}

			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				suite.Logger.WarnWith("Failed while reading api gateway response body",
					"apiGatewayURL", apiGatewayURL,
					"err", err)
				return false
			}

			if string(body) != "expected response" {
				suite.Logger.WarnWith("Got unexpected response from api gateway",
					"apiGatewayURL", apiGatewayURL,
					"body", string(body))
				return false
			}

			suite.Logger.DebugWith("Got expected response",
				"body", string(body))

			return true
		})
		suite.Require().NoError(err)

		return true
	}

	suite.DeployFunction(createFunctionOptions, afterFirstDeploy)
}

func (suite *APIGatewayTestSuite) TestDelete() {
	createFunctionOptions := suite.compileCreateFunctionOptions("test-api-gateway-func")

	functionSourceCode := base64.StdEncoding.EncodeToString([]byte(`def handler(context, event):
    return "expected response"
`))
	createFunctionOptions.FunctionConfig.Spec.Handler = "main:handler"
	createFunctionOptions.FunctionConfig.Spec.Runtime = "python:3.6"
	createFunctionOptions.FunctionConfig.Spec.Build.FunctionSourceCode = functionSourceCode

	// after the function is created, create the api gateway with it as its primary upstream
	afterFirstDeploy := func(deployResult *platform.CreateFunctionResult) bool {

		// get the function
		functions, err := suite.Platform.GetFunctions(&platform.GetFunctionsOptions{
			Name:      createFunctionOptions.FunctionConfig.Meta.Name,
			Namespace: createFunctionOptions.FunctionConfig.Meta.Namespace,
		})
		suite.Require().NoError(err)

		suite.Require().Lenf(functions, 1, "Unexpected amount of functions. actual: %s, expected: %s", len(functions), 1)

		function := functions[0]

		createAPIGatewayConfig := suite.compileCreateAPIGatewayOptions("test-api-gateway")
		createAPIGatewayConfig.APIGatewayConfig.Spec.Upstreams = []platform.APIGatewayUpstreamSpec{
			{
				Kind: platform.APIGatewayUpstreamKindNuclioFunction,
				Nucliofunction: &platform.NuclioFunctionAPIGatewaySpec{
					Name: function.GetConfig().Meta.Name,
				},
			},
		}

		suite.Logger.Debug("Creating api gateway")
		err = suite.Platform.CreateAPIGateway(createAPIGatewayConfig)
		suite.Require().NoError(err)

		suite.Logger.Debug("Deleting api gateway")
		err = suite.Platform.DeleteAPIGateway(&platform.DeleteAPIGatewayOptions{
			Meta: platform.APIGatewayMeta{
				Name: createAPIGatewayConfig.APIGatewayConfig.Meta.Name,
				Namespace: createAPIGatewayConfig.APIGatewayConfig.Meta.Namespace,
			},
		})
		suite.Require().NoError(err)

		// validate the api-gateway is successfully deleted as expected
		err = common.RetryUntilSuccessful(20 * time.Second, 1 * time.Second, func() bool {
			apiGateways, err := suite.Platform.GetAPIGateways(&platform.GetAPIGatewaysOptions{
				Name: createAPIGatewayConfig.APIGatewayConfig.Meta.Name,
				Namespace: createAPIGatewayConfig.APIGatewayConfig.Meta.Namespace,
			})
			if err != nil {
				suite.Logger.WarnWith("Failed to get api gateways. Retrying", "err", err)
				return false
			}

			if len(apiGateways) > 0 {
				suite.Logger.WarnWith("Api gateway still exists. Retrying")
				return false
			}

			suite.Logger.Debug("Api gateway was successfully deleted")
			return true
		})
		suite.Require().NoError(err)

		return true
	}

	suite.DeployFunction(createFunctionOptions, afterFirstDeploy)
}

func TestPlatformTestSuite(t *testing.T) {
	if testing.Short() {
		return
	}
	suite.Run(t, new(APIGatewayTestSuite))
	suite.Run(t, new(DeployFunctionTestSuite))
}
