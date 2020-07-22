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

package resource

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/nuclio/nuclio/pkg/dashboard"
	"github.com/nuclio/nuclio/pkg/platform"
	"github.com/nuclio/nuclio/pkg/restful"

	"github.com/nuclio/errors"
	"github.com/nuclio/nuclio-sdk-go"
)

type apiGatewayResource struct {
	*resource
}

type apiGatewayInfo struct {
	Meta   *platform.APIGatewayMeta   `json:"metadata,omitempty"`
	Spec   *platform.APIGatewaySpec   `json:"spec,omitempty"`
	Status *platform.APIGatewayStatus `json:"status,omitempty"`
}

// GetAll returns all api-gateways
func (agr *apiGatewayResource) GetAll(request *http.Request) (map[string]restful.Attributes, error) {
	response := map[string]restful.Attributes{}

	// get namespace
	namespace := agr.getNamespaceFromRequest(request)
	if namespace == "" {
		return nil, nuclio.NewErrBadRequest("Namespace must exist")
	}

	apiGateways, err := agr.getPlatform().GetAPIGateways(&platform.GetAPIGatewaysOptions{
		Meta: platform.APIGatewayMeta{
			Name:      request.Header.Get("x-nuclio-api-gateway-name"),
			Namespace: namespace,
		},
	})

	if err != nil {
		return nil, errors.Wrap(err, "Failed to get api-gateways")
	}

	// create a map of attributes keyed by the api-gateway id (name)
	for _, apiGateway := range apiGateways {
		response[apiGateway.GetConfig().Meta.Name] = agr.apiGatewayToAttributes(apiGateway)
	}

	return response, nil
}

// GetByID returns a specific api-gateway by id
func (agr *apiGatewayResource) GetByID(request *http.Request, id string) (restful.Attributes, error) {

	// get namespace
	namespace := agr.getNamespaceFromRequest(request)
	if namespace == "" {
		return nil, nuclio.NewErrBadRequest("Namespace must exist")
	}

	apiGateways, err := agr.getPlatform().GetAPIGateways(&platform.GetAPIGatewaysOptions{
		Meta: platform.APIGatewayMeta{
			Name:      id,
			Namespace: namespace,
		},
	})

	if err != nil {
		return nil, errors.Wrap(err, "Failed to get api-gateways")
	}

	if len(apiGateways) == 0 {
		return nil, nuclio.NewErrNotFound("Api-Gateway not found")
	}
	apiGateway := apiGateways[0]

	return agr.apiGatewayToAttributes(apiGateway), nil
}

// Create deploys a api-gateway
func (agr *apiGatewayResource) Create(request *http.Request) (id string, attributes restful.Attributes, responseErr error) {
	apiGatewayInfo, responseErr := agr.getAPIGatewayInfoFromRequest(request, true)
	if responseErr != nil {
		return
	}

	// create a api-gateway config
	apiGatewayConfig := platform.APIGatewayConfig{
		Meta:   *apiGatewayInfo.Meta,
		Spec:   *apiGatewayInfo.Spec,
		Status: *apiGatewayInfo.Status,
	}

	// create a api-gateway
	newAPIGateway, err := platform.NewAbstractAPIGateway(agr.Logger, agr.getPlatform(), apiGatewayConfig)
	if err != nil {
		return "", nil, nuclio.WrapErrInternalServerError(err)
	}

	// just deploy. the status is async through polling
	agr.Logger.DebugWith("Creating api-gateway", "newApi-Gateway", newAPIGateway)
	err = agr.getPlatform().CreateAPIGateway(&platform.CreateAPIGatewayOptions{
		APIGatewayConfig: *newAPIGateway.GetConfig(),
	})

	if err != nil {
		if strings.Contains(errors.Cause(err).Error(), "already exists") {
			return "", nil, nuclio.WrapErrConflict(err)
		}

		return "", nil, nuclio.WrapErrInternalServerError(err)
	}

	// set attributes
	attributes = agr.apiGatewayToAttributes(newAPIGateway)
	agr.Logger.DebugWith("Successfully created api-gateway",
		"id", id,
		"attributes", attributes)

	return
}

func (agr *apiGatewayResource) updateAPIGateway(request *http.Request) (*restful.CustomRouteFuncResponse, error) {

	statusCode := http.StatusNoContent

	// get api-gateway config and status from body
	apiGatewayInfo, err := agr.getAPIGatewayInfoFromRequest(request, true)
	if err != nil {
		agr.Logger.WarnWith("Failed to get api-gateway config and status from body", "err", err)

		return &restful.CustomRouteFuncResponse{
			Single:     true,
			StatusCode: http.StatusBadRequest,
		}, err
	}

	apiGatewayConfig := platform.APIGatewayConfig{
		Meta:   *apiGatewayInfo.Meta,
		Spec:   *apiGatewayInfo.Spec,
		Status: *apiGatewayInfo.Status,
	}

	err = agr.getPlatform().UpdateAPIGateway(&platform.UpdateAPIGatewayOptions{
		APIGatewayConfig: apiGatewayConfig,
	})

	if err != nil {
		agr.Logger.WarnWith("Failed to update api-gateway", "err", err)
	}

	// if there was an error, try to get the status code
	if err != nil {
		if errWithStatusCode, ok := err.(nuclio.ErrorWithStatusCode); ok {
			statusCode = errWithStatusCode.StatusCode()
		}
	}

	// return the stuff
	return &restful.CustomRouteFuncResponse{
		ResourceType: "api-gateway",
		Single:       true,
		StatusCode:   statusCode,
	}, err
}

// returns a list of custom routes for the resource
func (agr *apiGatewayResource) GetCustomRoutes() ([]restful.CustomRoute, error) {

	// since delete and update by default assume /resource/{id} and we want to get the id/namespace from the body
	// we need to register custom routes
	return []restful.CustomRoute{
		{
			Pattern:   "/",
			Method:    http.MethodPut,
			RouteFunc: agr.updateAPIGateway,
		},
		{
			Pattern:   "/",
			Method:    http.MethodDelete,
			RouteFunc: agr.deleteAPIGateway,
		},
	}, nil
}

func (agr *apiGatewayResource) createAPIGateway(apiGatewayInfoInstance *apiGatewayInfo) (id string,
	attributes restful.Attributes, responseErr error) {

	// create a api-gateway config
	apiGatewayConfig := platform.APIGatewayConfig{
		Meta:   *apiGatewayInfoInstance.Meta,
		Spec:   *apiGatewayInfoInstance.Spec,
		Status: *apiGatewayInfoInstance.Status,
	}

	// create a api-gateway
	newAPIGateway, err := platform.NewAbstractAPIGateway(agr.Logger, agr.getPlatform(), apiGatewayConfig)
	if err != nil {
		return "", nil, nuclio.WrapErrInternalServerError(err)
	}

	// just deploy. the status is async through polling
	agr.Logger.DebugWith("Creating api-gateway", "newApi-Gateway", newAPIGateway)
	err = agr.getPlatform().CreateAPIGateway(&platform.CreateAPIGatewayOptions{
		APIGatewayConfig: *newAPIGateway.GetConfig(),
	})

	if err != nil {
		if strings.Contains(errors.Cause(err).Error(), "already exists") {
			return "", nil, nuclio.WrapErrConflict(err)
		}

		return "", nil, nuclio.WrapErrInternalServerError(err)
	}

	// set attributes
	attributes = agr.apiGatewayToAttributes(newAPIGateway)
	agr.Logger.DebugWith("Successfully created api-gateway",
		"id", id,
		"attributes", attributes)
	return
}

func (agr *apiGatewayResource) deleteAPIGateway(request *http.Request) (*restful.CustomRouteFuncResponse, error) {

	// get api-gateway config and status from body
	apiGatewayInfo, err := agr.getAPIGatewayInfoFromRequest(request, true)
	if err != nil {
		agr.Logger.WarnWith("Failed to get api-gateway config and status from body", "err", err)

		return &restful.CustomRouteFuncResponse{
			Single:     true,
			StatusCode: http.StatusBadRequest,
		}, err
	}

	deleteAPIGatewayOptions := platform.DeleteAPIGatewayOptions{}
	deleteAPIGatewayOptions.Meta = *apiGatewayInfo.Meta

	err = agr.getPlatform().DeleteAPIGateway(&deleteAPIGatewayOptions)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errWithStatus, ok := err.(*nuclio.ErrorWithStatusCode); ok {
			statusCode = errWithStatus.StatusCode()
		}

		return &restful.CustomRouteFuncResponse{
			Single:     true,
			StatusCode: statusCode,
		}, err
	}

	return &restful.CustomRouteFuncResponse{
		ResourceType: "api-gateway",
		Single:       true,
		StatusCode:   http.StatusNoContent,
	}, err
}

func (agr *apiGatewayResource) apiGatewayToAttributes(apiGateway platform.APIGateway) restful.Attributes {
	attributes := restful.Attributes{
		"metadata": apiGateway.GetConfig().Meta,
		"spec":     apiGateway.GetConfig().Spec,
		"status":   apiGateway.GetConfig().Status,
	}

	return attributes
}

func (agr *apiGatewayResource) getNamespaceFromRequest(request *http.Request) string {
	return agr.getNamespaceOrDefault(request.Header.Get("x-nuclio-api-gateway-namespace"))
}

func (agr *apiGatewayResource) getAPIGatewayInfoFromRequest(request *http.Request, nameRequired bool) (*apiGatewayInfo, error) {

	// read body
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, nuclio.WrapErrInternalServerError(errors.Wrap(err, "Failed to read body"))
	}

	apiGatewayInfoInstance := apiGatewayInfo{}
	err = json.Unmarshal(body, &apiGatewayInfoInstance)
	if err != nil {
		return nil, nuclio.WrapErrBadRequest(errors.Wrap(err, "Failed to parse JSON body"))
	}

	err = agr.processAPIGatewayInfo(&apiGatewayInfoInstance, nameRequired)
	if err != nil {
		return nil, nuclio.WrapErrBadRequest(errors.Wrap(err, "Failed to process api-gateway info"))
	}

	return &apiGatewayInfoInstance, nil
}

func (agr *apiGatewayResource) processAPIGatewayInfo(apiGatewayInfoInstance *apiGatewayInfo, nameRequired bool) error {
	var err error

	// override namespace if applicable
	if apiGatewayInfoInstance.Meta != nil {
		apiGatewayInfoInstance.Meta.Namespace = agr.getNamespaceOrDefault(apiGatewayInfoInstance.Meta.Namespace)
	}

	// meta must exist
	if apiGatewayInfoInstance.Meta == nil ||
		(nameRequired && apiGatewayInfoInstance.Meta.Name == "") ||
		apiGatewayInfoInstance.Meta.Namespace == "" {

		if nameRequired {
			err = errors.New("Api-gateway name and namespace must be provided in metadata")
		} else {
			err = errors.New("Api-gateway namespace must be provided in metadata")
		}

		return nuclio.WrapErrBadRequest(err)
	}

	// spec must exist
	if apiGatewayInfoInstance.Spec == nil {
		err = errors.New("Api-gateway spec must be provided")

		return nuclio.WrapErrBadRequest(err)
	}

	// status is optional, ensure it exists
	if apiGatewayInfoInstance.Status == nil {
		apiGatewayInfoInstance.Status = &platform.APIGatewayStatus{}
	}

	return nil
}

// register the resource
var apiGatewayResourceInstance = &apiGatewayResource{
	resource: newResource("api/api_gateways", []restful.ResourceMethod{
		restful.ResourceMethodGetList,
		restful.ResourceMethodGetDetail,
		restful.ResourceMethodCreate,
		restful.ResourceMethodUpdate,
	}),
}

func init() {
	apiGatewayResourceInstance.Resource = apiGatewayResourceInstance
	apiGatewayResourceInstance.Register(dashboard.DashboardResourceRegistrySingleton)
}
