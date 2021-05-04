package iguazio

import (
	"fmt"
	"time"

	"github.com/nuclio/nuclio/pkg/platform"
	"github.com/nuclio/nuclio/pkg/platform/abstract/project"
	"github.com/nuclio/nuclio/pkg/platform/abstract/project/external/leader"
	"github.com/nuclio/nuclio/pkg/platformconfig"

	"github.com/nuclio/errors"
	"github.com/nuclio/logger"
)

type Synchronizer struct {
	logger                 logger.Logger
	platformConfiguration  *platformconfig.Config
	leaderClient           leader.Client
	internalProjectsClient project.Client
}

func NewSynchronizer(parentLogger logger.Logger,
	platformConfiguration *platformconfig.Config,
	leaderClient leader.Client,
	internalProjectsClient project.Client) (*Synchronizer, error) {

	parentLogger.DebugWith("Creating synchronizer")
	newSynchronizer := Synchronizer{
		logger:                 parentLogger.GetChild("leader-synchronizer-iguazio"),
		platformConfiguration:  platformConfiguration,
		leaderClient:           leaderClient,
		internalProjectsClient: internalProjectsClient,
	}

	return &newSynchronizer, nil
}

func (c *Synchronizer) Start() {
	c.logger.DebugWith("Starting outside sync")
	go c.synchronizationLoop()
}

func (c *Synchronizer) synchronizationLoop() {
	synchronizationInterval := c.platformConfiguration.ProjectsLeader.SynchronizationInterval
	if synchronizationInterval == 0 {
		c.logger.InfoWith("Synchronization interval set to 0. (projects will not synchronize with leader)")
		return
	}

	c.logger.InfoWith("Starting synchronization loop", "synchronizationInterval", synchronizationInterval)

	ticker := time.NewTicker(c.platformConfiguration.ProjectsLeader.SynchronizationInterval * time.Second)
	for {
		select {
		case _ = <-ticker.C:
			if err := c.synchronizeProjectsAccordingToLeader(); err != nil {
				c.logger.WarnWith("Failed to synchronize projects according to leader", "err", err)
			}
		}
	}
}

func (c *Synchronizer) getModifiedProjects(leaderProjects []platform.Project, internalProjects []platform.Project) (
	projectsToCreate []*platform.ProjectConfig,
	projectsToUpdate []*platform.ProjectConfig) {

	// create a mapping of all existing internal projects
	internalProjectsMap := map[string]*platform.ProjectConfig{}
	for _, internalProject := range internalProjects {
		internalProjectConfig := internalProject.GetConfig()
		c.logger.DebugWith("Internal project config instance", "internalProjectConfig", *internalProjectConfig)
		if internalProjectConfig == nil {
			continue
		}

		// generate a unique namespace+name key for the project
		namespaceAndNameKey := fmt.Sprintf("%s:%s",
			internalProjectConfig.Meta.Namespace,
			internalProjectConfig.Meta.Name)

		internalProjectsMap[namespaceAndNameKey] = internalProjectConfig
	}

	// iterate over matching leader projects and create/update each according to the existing internal projects
	for _, leaderProject := range leaderProjects {
		leaderProjectConfig := leaderProject.GetConfig()
		c.logger.DebugWith("Leader project config instance", "leaderProjectConfig", *leaderProjectConfig)
		if leaderProjectConfig == nil ||
			leaderProjectConfig.Status.OperationalStatus != "online" ||
			leaderProjectConfig.Status.AdminStatus != "online" {
			c.logger.DebugWith("Skipping non online project", "status", leaderProjectConfig.Status)
			continue
		}

		// generate a unique namespace+name key for the project (same as above)
		namespaceAndNameKey := fmt.Sprintf("%s:%s",
			leaderProjectConfig.Meta.Namespace,
			leaderProjectConfig.Meta.Name)

		_, found := internalProjectsMap[namespaceAndNameKey]
		if found {

			// if the project exists both internally and on the leader - update it
			projectsToUpdate = append(projectsToUpdate, leaderProjectConfig)
		} else {
			projectsToCreate = append(projectsToCreate, leaderProjectConfig)
		}
	}

	return
}

func (c *Synchronizer) getDeletedProjects(leaderProjects []Project,
	internalProjects []platform.Project) []platform.ProjectConfig {

	return nil
}

func (c *Synchronizer) getUpdatedProjects(leaderProjects []Project,
	internalProjects []platform.Project) []platform.ProjectConfig {

	return nil
}

func (c *Synchronizer) synchronizeProjectsAccordingToLeader() error {

	// fetch projects from leader
	leaderProjects, err := c.leaderClient.GetAll()
	if err != nil {
		return errors.Wrap(err, "Failed to get leader projects")
	}
	if leaderProjects == nil && err == nil {
		c.logger.DebugWith("Skipping cause nil nil")
		return nil
	}

	// fetch internal projects
	// TODO: fetch projects from every managed namespace - could be done by implementing GetAll()
	namespace := "default-tenant"
	internalProjects, err :=
		c.internalProjectsClient.Get(&platform.GetProjectsOptions{Meta: platform.ProjectMeta{Namespace: "default-tenant"}})
	if err != nil {
		return errors.Wrapf(err, "Failed to get internal projects from namespace: %s", namespace)
	}

	// filter modified projects
	projectsToCreate, projectsToUpdate := c.getModifiedProjects(leaderProjects, internalProjects)
	c.logger.DebugWith("Got synchronization loop modified projects",
		"projectsToCreateNum", len(projectsToCreate),
		"projectsToUpdateNum", len(projectsToUpdate))

	// create projects that exist on the leader but weren't created internally
	for _, projectInstance := range projectsToCreate {
		projectInstance := projectInstance
		c.logger.DebugWith("Syncing create project", "projectInstance", *projectInstance)
		//go func() {
		//	createProjectConfig := &platform.CreateProjectOptions{
		//		ProjectConfig: &platform.ProjectConfig{
		//			Meta: projectInstance.Meta,
		//			Spec: projectInstance.Spec},
		//	}
		//	if _, err := c.internalProjectsClient.Create(createProjectConfig); err != nil {
		//		c.logger.WarnWith("Failed to create project (during sync)",
		//			"name", createProjectConfig.ProjectConfig.Meta.Name,
		//			"namespace", createProjectConfig.ProjectConfig.Meta.Namespace,
		//			"err", err)
		//	}
		//}()
	}

	// update projects that exist both internally and on the leader
	for _, projectInstance := range projectsToUpdate {
		projectInstance := projectInstance
		c.logger.DebugWith("Syncing update project", "projectInstance", *projectInstance)
		//go func() {
		//	updateProjectOptions := &platform.UpdateProjectOptions{
		//		ProjectConfig: platform.ProjectConfig{
		//			Meta: projectInstance.Meta,
		//			Spec: projectInstance.Spec,
		//		},
		//	}
		//	if _, err := c.internalProjectsClient.Update(updateProjectOptions); err != nil {
		//		c.logger.WarnWith("Failed to update project (during sync)",
		//			"name", updateProjectOptions.ProjectConfig.Meta.Name,
		//			"namespace", updateProjectOptions.ProjectConfig.Meta.Namespace,
		//			"err", err)
		//	}
		//}()
	}

	return nil
}
