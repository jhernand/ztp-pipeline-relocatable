/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package testing

import (
	"context"
	"sync"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/ztp-pipeline-relocatable/ztp/internal/logging"
)

var (
	globalEnvLock = &sync.Mutex{}
	globalEnvRC   = 0
	globalEnv     *Environment
)

// BorrowEnv returns the global testing environment and increases the reference count. Remember to
// release it calling the ReleaseEnv function, otherwise the environment may not be deleted when the
// tests finish.
func BorrowEnv() *Environment {
	globalEnvLock.Lock()
	defer globalEnvLock.Unlock()
	if globalEnv == nil {
		globalEnv = makeGlobalEnv()
	}
	return globalEnv
}

// ReleaseEnv releases the global testing environment and decreases the reference count. When the
// count reaches zero the environment will be stoped.
func ReleaseEnv() {
	globalEnvLock.Lock()
	defer globalEnvLock.Unlock()
	if globalEnvRC > 0 {
		globalEnvRC--
		if globalEnvRC == 0 {
			err := globalEnv.Stop(context.Background())
			Expect(err).ToNot(HaveOccurred())
			globalEnv = nil
		}
	}
}

func makeGlobalEnv() *Environment {
	// Create the logger:
	logger, err := logging.NewLogger().
		SetWriter(GinkgoWriter).
		SetV(2).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create the environment:
	env, err := NewEnvironment().
		SetLogger(logger).
		SetName("ztp").
		Build()
	Expect(err).ToNot(HaveOccurred())

	return env
}
