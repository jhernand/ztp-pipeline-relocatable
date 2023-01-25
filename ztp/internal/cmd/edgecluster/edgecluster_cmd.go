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

package edgecluster

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rh-ecosystem-edge/ztp-pipeline-relocatable/ztp/internal"
	"github.com/rh-ecosystem-edge/ztp-pipeline-relocatable/ztp/internal/models"
	"github.com/rh-ecosystem-edge/ztp-pipeline-relocatable/ztp/internal/templating"
)

//go:embed templates
var templatesFS embed.FS

// Cobra creates and returns the `edgecluster` command.
func Cobra() *cobra.Command {
	c := NewCommand()
	return &cobra.Command{
		Use:   "edgecluster",
		Short: "Creates an edge cluster",
		Long:  "Creates an edge cluster",
		Args:  cobra.NoArgs,
		RunE:  c.Run,
	}
}

// Command contains the data and logic needed to run the `edgecluster` command.
type Command struct {
	logger    logr.Logger
	env       map[string]string
	jq        *internal.JQ
	tool      *internal.Tool
	config    models.Config
	client    clnt.WithWatch
	templates *templating.Engine
	enricher  *internal.Enricher
	renderer  *internal.Renderer
}

// NewCommand creates a new runner that knows how to execute the `edgecluster` command.
func NewCommand() *Command {
	return &Command{}
}

// Run runs the `edgecluster` command.
func (c *Command) Run(cmd *cobra.Command, argv []string) (err error) {
	// Get the context:
	ctx := cmd.Context()

	// Get the dependencies from the context:
	c.logger = internal.LoggerFromContext(ctx)
	c.tool = internal.ToolFromContext(ctx)

	// Get the environment:
	c.env = c.tool.Env()

	// Create the JQ object:
	c.jq, err = internal.NewJQ().
		SetLogger(c.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create JQ object: %v", err)
		return
	}

	// Load the templates:
	c.templates, err = templating.NewEngine().
		SetLogger(c.logger).
		SetFS(templatesFS).
		SetDir("templates").
		Build()
	if err != nil {
		err = fmt.Errorf("failed to parse the templates: %v", err)
		return
	}

	// Find the subset of templates that contain the objects:
	templates := []string{}
	for _, name := range c.templates.Names() {
		if strings.HasPrefix(name, "objects/") && strings.HasSuffix(name, ".yaml") {
			templates = append(templates, name)
		}
	}

	// Load the configuration:
	file, ok := c.tool.Env()["EDGECLUSTERS_FILE"]
	if !ok {
		err = fmt.Errorf(
			"failed to load configuration because environment variable " +
				"'EDGECLUSTERS_FILE' isn't defined",
		)
		return
	}
	fmt.Fprintf(c.tool.Out(), "Loading configuration\n")
	c.config, err = internal.NewConfigLoader().
		SetLogger(c.logger).
		SetSource(file).
		Load()
	if err != nil {
		err = fmt.Errorf(
			"failed to load configuration from file '%s': %v",
			file, err,
		)
		return
	}

	// Create the client for the API:
	c.client, err = internal.NewClient().
		SetLogger(c.logger).
		SetEnv(c.env).
		Build()
	if err != nil {
		err = fmt.Errorf(
			"failed to create API client: %v",
			err,
		)
		return
	}

	// Create the enricher and the renderer:
	c.enricher, err = internal.NewEnricher().
		SetLogger(c.logger).
		SetEnv(c.env).
		SetClient(c.client).
		Build()
	if err != nil {
		err = fmt.Errorf(
			"failed to create enricher: %v",
			err,
		)
		return
	}
	c.renderer, err = internal.NewRenderer().
		SetLogger(c.logger).
		SetTemplates(c.templates, templates...).
		Build()
	if err != nil {
		err = fmt.Errorf(
			"failed to create renderer: %v",
			err,
		)
		return
	}

	// Deploy the clusters:
	for _, cluster := range c.config.Clusters {
		err = c.deploy(ctx, &cluster)
		if err != nil {
			err = fmt.Errorf(
				"failed to deploy cluster '%s': %v",
				cluster.Name, err,
			)
			return
		}
	}

	// Check if it is necessary to deploy:
	/*
		ok, err = c.verify(ctx)
		if err != nil {
			return
		}
		if !ok {
			fmt.Fprintf(c.tool.Out(), ">> Cluster deployed, this step is not neccessary\n")
			return
		}
	*/

	return
}

func (c *Command) deploy(ctx context.Context, cluster *models.Cluster) error {
	// Complete the description of the cluster:
	err := c.enricher.Enrich(ctx, cluster)
	if err != nil {
		return err
	}
	c.logger.V(2).Info(
		"Enriched cluster",
		"cluster", cluster,
	)

	// Render the objects:
	objects, err := c.renderer.Render(ctx, map[string]any{
		"Cluster": cluster,
	})
	if err != nil {
		return err
	}
	c.logger.V(2).Info(
		"Rendered objects",
		"objects", objects,
	)

	// Create the objects:
	for _, object := range objects {
		err = c.client.Create(ctx, object)
		if err != nil {
			return err
		}
	}

	/*
	   oc patch provisioning provisioning-configuration --type merge -p '{"spec":{"watchAllNamespaces": true}}'

	   echo "Verifying again the clusterDeployment"
	   # Waiting for 166 min to have the cluster deployed
	   ${WORKDIR}/${DEPLOY_EDGECLUSTERS_DIR}/verify.sh 10000
	*/

	return nil
}

func (c *Command) verify(ctx context.Context) (result bool, err error) {
	for _, cluster := range c.config.Clusters {
		fmt.Fprintf(c.tool.Out(), ">>>> Starting the validation until finish the installation\n")
		fmt.Fprintf(c.tool.Out(), ">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>\n")
		result, err = c.checkBMHS(ctx, cluster)
		if err != nil || !result {
			return
		}
		result, err = c.checkACI(ctx, cluster)
		if err != nil || !result {
			return
		}
		fmt.Fprintf(c.tool.Out(), ">>>>EOF\n")
		fmt.Fprintf(c.tool.Out(), ">>>>>>>\n")
	}
	result = true
	return
}

func (c *Command) checkBMHS(ctx context.Context, cluster models.Cluster) (result bool, err error) {
	for {
		// Return when the timeout expires:
		select {
		case <-ctx.Done():
			fmt.Fprintf(c.tool.Out(), "timeout waiting for BMH to be provisioned")
			return
		default:
		}

		// Get the list of bare metal hosts:
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "metal3.io",
			Version: "v1alpha1",
			Kind:    "BareMetalHostList",
		})
		err = c.client.List(ctx, list, clnt.InNamespace(cluster.Name))
		if err != nil {
			return
		}

		// Get the provisioning state of the hosts:
		var states []string
		err = c.jq.Query(`.items[].status.provisioning.state`, list, &states)
		if err != nil {
			return
		}

		// Check that all the host are provisioned:
		ready := true
		for _, state := range states {
			if state != "provisioned" {
				ready = false
				break
			}
		}
		if ready {
			fmt.Fprintf(c.tool.Out(), "BMH's for %s verified\n", cluster.Name)
			result = true
			return
		}

		fmt.Fprintf(
			c.tool.Out(),
			">> Waiting for BMH on edgecluster for each cluster node: %v\n",
			states,
		)

		time.Sleep(30 * time.Second)
	}
}

func (c *Command) checkACI(ctx context.Context, cluster models.Cluster) (result bool, err error) {
	var status string
	for {
		// Return when the timeout expires:
		select {
		case <-ctx.Done():
			fmt.Fprintf(
				c.tool.Out(),
				"Timeout waiting for AgentClusterInstall %s on condition "+
					".status.conditions.Completed",
				cluster.Name,
			)
			fmt.Fprintf(
				c.tool.Out(),
				"Expected: True Current: %s\n",
				status,
			)
			return
		default:
		}

		// Get the agent cluster install:
		object := &unstructured.Unstructured{}
		object.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "extensions.hive.openshift.io",
			Version: "v1beta1",
			Kind:    "AgentClusterInstall",
		})
		key := clnt.ObjectKey{
			Namespace: cluster.Name,
			Name:      cluster.Name,
		}
		err = c.client.Get(ctx, key, object)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return
		}

		// Get the status:
		err = c.jq.Query(
			`.status.conditions[] | select(.type == "Completed") | .status`,
			object, &status,
		)
		if err != nil {
			return
		}
		if strings.EqualFold(status, "True") {
			fmt.Fprintf(
				c.tool.Out(),
				"AgentClusterInstall for %s verified\n",
				cluster.Name,
			)
			result = true
			return
		}

		// Get the state info:
		var stateInfo string
		err = c.jq.Query(
			`.status.debugInfo.stateInfo`,
			object, &stateInfo,
		)
		if err != nil {
			return
		}

		fmt.Fprintf(c.tool.Out(), ">> Waiting for ACI\n")
		fmt.Fprintf(c.tool.Out(), "Edgecluster: %s\n", cluster.Name)
		fmt.Fprintf(c.tool.Out(), "Current: %s\n", stateInfo)
		fmt.Fprintf(c.tool.Out(), "Desired State: Cluster is Installed\n")
		fmt.Fprintf(c.tool.Out(), "\n")

		time.Sleep(30 * time.Second)
	}
}
