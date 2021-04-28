package iguazio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nuclio/nuclio/pkg/common"
	"github.com/nuclio/nuclio/pkg/platform"
	"github.com/nuclio/nuclio/pkg/platformconfig"

	"github.com/nuclio/errors"
	"github.com/nuclio/logger"
)

const (
	ProjectsRoleHeaderKey         = "x-projects-role"
	ProjectsRoleHeaderValueNuclio = "nuclio"
	DefaultRequestTimeout         = 10 * time.Second
)

type Client struct {
	logger                logger.Logger
	platformConfiguration *platformconfig.Config
	cachedCookie          *http.Cookie
}

func NewClient(parentLogger logger.Logger, platformConfiguration *platformconfig.Config) (*Client, error) {
	newClient := Client{
		logger:                parentLogger.GetChild("leader-client-iguazio"),
		platformConfiguration: platformConfiguration,
	}

	return &newClient, nil
}

func (c *Client) Create(createProjectOptions *platform.CreateProjectOptions) error {
	c.logger.DebugWith("Sending create project request to leader",
		"name", createProjectOptions.ProjectConfig.Meta.Name,
		"namespace", createProjectOptions.ProjectConfig.Meta.Namespace)

	c.cachedCookie = createProjectOptions.SessionCookie

	// generate request body
	body, err := c.generateProjectRequestBody(createProjectOptions.ProjectConfig)
	if err != nil {
		return errors.Wrap(err, "Failed to generate project request body")
	}

	// send the request
	headers := c.generateCommonRequestHeaders()
	responseBody, _, err := common.SendHTTPRequest(http.MethodPost,
		fmt.Sprintf("%s/%s", c.platformConfiguration.ProjectsLeader.APIAddress, "projects"),
		body,
		headers,
		[]*http.Cookie{createProjectOptions.SessionCookie},
		http.StatusCreated,
		true,
		DefaultRequestTimeout)
	if err != nil {
		return errors.Wrap(err, "Failed to send request to leader")
	}

	c.logger.DebugWith("Successfully sent create project request to leader",
		"name", createProjectOptions.ProjectConfig.Meta.Name,
		"namespace", createProjectOptions.ProjectConfig.Meta.Namespace,
		"responseBody", string(responseBody))

	return nil
}

func (c *Client) Update(updateProjectOptions *platform.UpdateProjectOptions) error {
	c.logger.DebugWith("Sending update project request to leader",
		"name", updateProjectOptions.ProjectConfig.Meta.Name,
		"namespace", updateProjectOptions.ProjectConfig.Meta.Namespace)

	c.cachedCookie = updateProjectOptions.SessionCookie

	// generate request body
	body, err := c.generateProjectRequestBody(&updateProjectOptions.ProjectConfig)
	if err != nil {
		return errors.Wrap(err, "Failed to generate project request body")
	}

	// send the request
	headers := c.generateCommonRequestHeaders()
	responseBody, _, err := common.SendHTTPRequest(http.MethodPut,
		fmt.Sprintf("%s/%s/%s",
			c.platformConfiguration.ProjectsLeader.APIAddress,
			"projects/__name__",
			updateProjectOptions.ProjectConfig.Meta.Name),
		body,
		headers,
		[]*http.Cookie{updateProjectOptions.SessionCookie},
		http.StatusOK,
		true,
		DefaultRequestTimeout)
	if err != nil {
		return errors.Wrap(err, "Failed to send request to leader")
	}

	c.logger.DebugWith("Successfully sent update project request to leader",
		"name", updateProjectOptions.ProjectConfig.Meta.Name,
		"namespace", updateProjectOptions.ProjectConfig.Meta.Namespace,
		"responseBody", string(responseBody))

	return nil
}

func (c *Client) Delete(deleteProjectOptions *platform.DeleteProjectOptions) error {
	c.logger.DebugWith("Sending delete project request to leader",
		"name", deleteProjectOptions.Meta.Name)

	c.cachedCookie = deleteProjectOptions.SessionCookie

	// generate request body
	body, err := c.generateProjectDeletionRequestBody(deleteProjectOptions.Meta.Name)
	if err != nil {
		return errors.Wrap(err, "Failed to generate project deletion request body")
	}

	// send the request
	headers := c.generateCommonRequestHeaders()
	headers["igz-project-deletion-strategy"] = string(deleteProjectOptions.Strategy)
	if _, _, err := common.SendHTTPRequest(http.MethodDelete,
		fmt.Sprintf("%s/%s", c.platformConfiguration.ProjectsLeader.APIAddress, "projects"),
		body,
		headers,
		[]*http.Cookie{deleteProjectOptions.SessionCookie},
		http.StatusAccepted,
		true,
		DefaultRequestTimeout); err != nil {

		return errors.Wrap(err, "Failed to send request to leader")
	}

	c.logger.DebugWith("Successfully sent delete project request to leader",
		"name", deleteProjectOptions.Meta.Name,
		"namespace", deleteProjectOptions.Meta.Namespace)

	return nil
}

func (c *Client) GetAll() ([]platform.Project, error) {
	c.logger.DebugWith("Sending get all projects request to leader")

	// send the request
	headers := c.generateCommonRequestHeaders()
	responseBody, _, err := common.SendHTTPRequest(http.MethodGet,
		fmt.Sprintf("%s/%s", c.platformConfiguration.ProjectsLeader.APIAddress, "projects"),
		nil,
		headers,
		[]*http.Cookie{c.cachedCookie},
		http.StatusOK,
		true,
		DefaultRequestTimeout)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to send request to leader")
	}

	var projectsList ProjectList
	if err := json.Unmarshal(responseBody, projectsList); err != nil {
		return nil, errors.Wrap(err, "Failed to unmarshal response body")
	}

	return projectsList.ToSingleProjectList(), nil
}

func (c *Client) generateCommonRequestHeaders() map[string]string {
	return map[string]string{
		ProjectsRoleHeaderKey: ProjectsRoleHeaderValueNuclio,
		"Content-Type":        "application/json",
	}
}

func (c *Client) generateProjectRequestBody(projectConfig *platform.ProjectConfig) ([]byte, error) {
	project := Project{
		Data: ProjectData{
			Type: ProjectType,
			Attributes: ProjectAttributes{
				Name:        projectConfig.Meta.Name,
				Labels:      projectConfig.Meta.Labels,
				Annotations: projectConfig.Meta.Annotations,
				Description: projectConfig.Spec.Description,
			},
		},
	}

	c.enrichProjectWithNuclioFields(&project)

	return json.Marshal(project)
}

func (c *Client) enrichProjectWithNuclioFields(project *Project) {

	// TODO: update this function when nuclio fields are added
	//project.Data.Attributes.NuclioProject = NuclioProject{}
}

func (c *Client) generateProjectDeletionRequestBody(projectName string) ([]byte, error) {
	return json.Marshal(Project{
		Data: ProjectData{
			Type: ProjectType,
			Attributes: ProjectAttributes{
				Name: projectName,
			},
		},
	})
}
