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

package trigger

import (
	"github.com/nuclio/nuclio/pkg/functionconfig"
	"github.com/nuclio/nuclio/pkg/processor/runtime"
	"github.com/nuclio/nuclio/pkg/processor/worker"
	"github.com/nuclio/nuclio/pkg/registry"

	"github.com/nuclio/logger"
)

// Creator creates a trigger instance
type Creator interface {

	// Create creates a trigger instance
	Create(logger.Logger, string, *functionconfig.Trigger, *runtime.Configuration, map[string]worker.Allocator) (Trigger, error)
}

type Registry struct {
	registry.Registry
}

// global singleton
var RegistrySingleton = Registry{
	Registry: *registry.NewRegistry("trigger"),
}

func (r *Registry) NewTrigger(logger logger.Logger,
	kind string,
	name string,
	triggerConfiguration *functionconfig.Trigger,
	runtimeConfiguration *runtime.Configuration,
	namedWorkerAllocators map[string]worker.Allocator) (Trigger, error) {

	registree, err := r.Get(kind)
	if err != nil {
		return nil, err
	}

	// set trigger name for runtime config, this can be overridden by the trigger if it pleases
	runtimeConfiguration.TriggerName = name

	trigger, err := registree.(Creator).Create(logger,
		name,
		triggerConfiguration,
		runtimeConfiguration,
		namedWorkerAllocators)
	if err != nil {
		logger.ErrorWith("Failed to create trigger",
			"kind", kind,
			"triggerName", name)
		logger.ErrorWith("Failed to create trigger2",
			"kind", kind)
		logger.WarnWith("Failed to create trigger - warn",
			"kind", kind)
	}

	return trigger, err
}
