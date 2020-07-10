package controller

import (
	"fmt"
	"strings"
	"time"

	"github.com/nuclio/logger"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CronJobMonitoring struct {
	logger                           logger.Logger
	controller                       *Controller
	staleCronJobPodsDeletionInterval *time.Duration
}

func NewCronJobMonitoring(parentLogger logger.Logger,
	controller *Controller,
	staleCronJobPodsDeletionInterval *time.Duration) *CronJobMonitoring {

	loggerInstance := parentLogger.GetChild("cron_job_monitoring")

	newCronJobMonitoring := &CronJobMonitoring{
		logger:                           loggerInstance,
		controller:                       controller,
		staleCronJobPodsDeletionInterval: staleCronJobPodsDeletionInterval,
	}

	parentLogger.DebugWith("Successfully created cron job monitoring instance",
		"staleCronJobPodsDeletionInterval", staleCronJobPodsDeletionInterval)

	return newCronJobMonitoring
}

func (cjpd *CronJobMonitoring) start() {

	go cjpd.startStaleCronJobPodsDeletionLoop() // nolint: errcheck

}

// delete all stale cron job pods (as k8s lacks this logic, and CronJob pods are never deleted)
func (cjpd *CronJobMonitoring) startStaleCronJobPodsDeletionLoop() error {
	stalePodsFieldSelector := cjpd.compileStalePodsFieldSelector()

	cjpd.logger.InfoWith("Starting stale cron job pods deletion loop",
		"staleCronJobPodsDeletionInterval", cjpd.staleCronJobPodsDeletionInterval,
		"fieldSelectors", stalePodsFieldSelector)

	for {

		// sleep until next deletion time staleCronJobPodsDeletionInterval
		time.Sleep(*cjpd.staleCronJobPodsDeletionInterval)

		cronJobPods, err := cjpd.controller.kubeClientSet.
			CoreV1().
			Pods(cjpd.controller.namespace).
			List(metav1.ListOptions{
					LabelSelector: "nuclio.io/function-cron-job-pod=true",
					FieldSelector: stalePodsFieldSelector,
				})
		if err != nil {
			cjpd.logger.WarnWith("Failed to list stale cron job pods", "err", err)
			continue
		}

		for _, cronJobPod := range cronJobPods.Items {

			// if this pod is in pending for too long - delete it too
			if cronJobPod.Status.Phase == v1.PodPending &&
				cronJobPod.ObjectMeta.CreationTimestamp.After(time.Now().Add(-time.Minute)) {
				continue
			}

			err := cjpd.controller.kubeClientSet.
				CoreV1().
				Pods(cronJobPod.Namespace).
				Delete(cronJobPod.Name, &metav1.DeleteOptions{})
			cjpd.logger.WarnWith("Failed to cleanup stale cron job pod",
				"podName", cronJobPod.Name,
				"err", err)
		}
	}
}

// create a field selector(string) for stale pods
func (cjpd *CronJobMonitoring) compileStalePodsFieldSelector() string {
	var fieldSelectors []string

	// filter out non stale pods by their phase
	nonStalePodPhases := []v1.PodPhase{v1.PodRunning}
	for _, nonStalePodPhase := range nonStalePodPhases {
		selector := fmt.Sprintf("status.phase!=%s", string(nonStalePodPhase))
		fieldSelectors = append(fieldSelectors, selector)
	}

	return strings.Join(fieldSelectors, ",")
}
