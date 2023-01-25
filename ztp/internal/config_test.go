/*
Copyright 2022 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package internal

import (
	"math"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/ztp-pipeline-relocatable/ztp/internal/logging"
	. "github.com/rh-ecosystem-edge/ztp-pipeline-relocatable/ztp/internal/testing"
)

var _ = Describe("Config", func() {
	var logger logr.Logger

	BeforeEach(func() {
		var err error
		logger, err = logging.NewLogger().
			SetWriter(GinkgoWriter).
			SetV(math.MaxInt).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Loads simple SNO cluster", func() {
		// Load the configuration:
		config, err := NewConfigLoader().
			SetLogger(logger).
			SetSource(Dedent(`
				edgeclusters:
				- my-sno:
				    master0:
				      ignore_ifaces: eno1 eno2
				      nic_ext_dhcp: eno4
				      mac_ext_dhcp: f8:1a:0e:f8:6a:f2
				      bmc_url: http://192.168.122.1/my-bmc
				      bmc_user: my-user
				      bmc_pass: my-pass
				      root_disk: /dev/sda
			`)).
			Load()
		Expect(err).ToNot(HaveOccurred())

		// Verify it:
		Expect(config.Clusters).To(HaveLen(1))
	})
})
