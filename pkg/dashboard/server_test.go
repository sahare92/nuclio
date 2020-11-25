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

package dashboard

import (
	"testing"
	"time"

	"github.com/nuclio/nuclio/pkg/dockercreds"
	"github.com/nuclio/nuclio/pkg/functionconfig"
	"github.com/nuclio/nuclio/pkg/platform"
	mockplatform "github.com/nuclio/nuclio/pkg/platform/mock"

	"github.com/nuclio/logger"
	"github.com/nuclio/zap"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type DashboardServerTestSuite struct {
	suite.Suite
	Server
	mockPlatform *mockplatform.Platform
	Logger       logger.Logger
}

func (suite *DashboardServerTestSuite) SetupTest() {
	var err error

	suite.Logger, err = nucliozap.NewNuclioZapTest("test")
	suite.Require().NoError(err)

	suite.mockPlatform = &mockplatform.Platform{}
	suite.Platform = suite.mockPlatform
}

func (suite *DashboardServerTestSuite) TestResolveRegistryURLFromDockerCredentials() {
	dummyUsername := "dummy-user"
	for _, testCase := range []struct {
		credentials             dockercreds.Credentials
		expectedRegistryURLHost string
		Match                   bool
	}{
		{
			credentials:             dockercreds.Credentials{URL: "https://index.docker.io/v1/", Username: dummyUsername},
			expectedRegistryURLHost: "index.docker.io",
		},
		{
			credentials:             dockercreds.Credentials{URL: "index.docker.io/v1/", Username: dummyUsername},
			expectedRegistryURLHost: "index.docker.io",
		},
		{
			credentials:             dockercreds.Credentials{URL: "https://index.docker.io", Username: dummyUsername},
			expectedRegistryURLHost: "index.docker.io",
		},
		{
			credentials:             dockercreds.Credentials{URL: "index.docker.io", Username: dummyUsername},
			expectedRegistryURLHost: "index.docker.io",
		},
	} {
		expectedRegistryURL := testCase.expectedRegistryURLHost + "/" + dummyUsername
		suite.Require().Equal(expectedRegistryURL, suite.resolveDockerCredentialsRegistryURL(testCase.credentials))
	}
}

func (suite *DashboardServerTestSuite) TestUpdateStuckFunctionsState() {

	// Mock returned functions

	// a function that is stuck on building state (its state should be set to error)
	now := time.Now()
	returnedFunctionStuckOnBuilding := platform.AbstractFunction{}
	returnedFunctionStuckOnBuilding.Config.Meta.Name = "f1"
	returnedFunctionStuckOnBuilding.Config.Meta.Namespace = "default-namespace"
	returnedFunctionStuckOnBuilding.Status.State = functionconfig.FunctionStateBuilding
	returnedFunctionStuckOnBuilding.Status.LastBuildTime = &now

	// a function that is on building state but doesn't have last build time (should be skipped)
	returnedFunctionOnBuildingNoLastBuildTime := platform.AbstractFunction{}
	returnedFunctionOnBuildingNoLastBuildTime.Config.Meta.Name = "f2"
	returnedFunctionOnBuildingNoLastBuildTime.Config.Meta.Namespace = "default-namespace"
	returnedFunctionOnBuildingNoLastBuildTime.Status.State = functionconfig.FunctionStateBuilding

	// a function that is on ready state (should be skipped)
	returnedFunctionReady := platform.AbstractFunction{}
	returnedFunctionReady.Config.Meta.Name = "f3"
	returnedFunctionReady.Config.Meta.Namespace = "default-namespace"
	returnedFunctionReady.Status.State = functionconfig.FunctionStateReady

	// a function that is on building state that is still being built (should be skipped)
	returnedFunctionNotStuckOnBuilding := platform.AbstractFunction{}
	returnedFunctionNotStuckOnBuilding.Config.Meta.Name = "f4"
	returnedFunctionNotStuckOnBuilding.Config.Meta.Namespace = "default-namespace"
	returnedFunctionNotStuckOnBuilding.Status.State = functionconfig.FunctionStateBuilding
	laterThanValidationTime := time.Now().Add(1 * time.Second)
	returnedFunctionNotStuckOnBuilding.Status.LastBuildTime = &laterThanValidationTime

	suite.defaultNamespace = "default-namespace"

	// mock sleep duration to be lower so the test won't take long
	suite.updateFunctionsStuckOnBuildingSleepTime = 2 * time.Second

	// verify
	verifyGetFunctions := func(getFunctionsOptions *platform.GetFunctionsOptions) bool {
		suite.Require().Equal("default-namespace", getFunctionsOptions.Namespace)

		return true
	}
	verifyUpdateFunction := func(updateFunctionsOptions *platform.UpdateFunctionOptions) bool {
		suite.Require().Equal(returnedFunctionStuckOnBuilding.GetConfig().Meta.Name, updateFunctionsOptions.FunctionMeta.Name)
		suite.Require().Equal(functionconfig.FunctionStateError, updateFunctionsOptions.FunctionStatus.State)
		suite.Require().Equal("Function found stuck on building state (Perhaps nuclio dashboard went down"+
			" during function deployment. Try to redeploy)", updateFunctionsOptions.FunctionStatus.Message)

		return true
	}

	// mock returned functions
	suite.mockPlatform.
		On("GetFunctions", mock.MatchedBy(verifyGetFunctions)).
		Return([]platform.Function{&returnedFunctionStuckOnBuilding,
			&returnedFunctionOnBuildingNoLastBuildTime,
			&returnedFunctionNotStuckOnBuilding,
			&returnedFunctionReady}, nil).
		Once()

	// mock update function - expect it to be called only once, for the function with "building" state
	suite.mockPlatform.
		On("UpdateFunction", mock.MatchedBy(verifyUpdateFunction)).
		Return(nil).
		Once()

	// run the update function (the tested function)
	suite.updateFunctionsStuckOnBuildingState()
}

func TestDashboardServerTestSuite(t *testing.T) {
	suite.Run(t, new(DashboardServerTestSuite))
}
