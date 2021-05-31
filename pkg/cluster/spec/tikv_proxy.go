// Copyright 2020 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package spec

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pingcap/tiup/pkg/cluster/ctxt"
	"github.com/pingcap/tiup/pkg/cluster/template/scripts"
	"github.com/pingcap/tiup/pkg/meta"
)

// TikvProxySpec represents the Tikv-Proxy topology specification in topology.yaml
type TikvProxySpec struct {
	Host       string `yaml:"host"`
	ListenHost string `yaml:"listen_host,omitempty"`
	SSHPort    int    `yaml:"ssh_port,omitempty" validate:"ssh_port:editable"`
	Imported   bool   `yaml:"imported,omitempty"`
	Patched    bool   `yaml:"patched,omitempty"`
	Port       int    `yaml:"port" default:"21000"`
	//StatusPort      int                    `yaml:"status_port" default:"10080"`
	DeployDir       string                 `yaml:"deploy_dir,omitempty"`
	LogDir          string                 `yaml:"log_dir,omitempty"`
	NumaNode        string                 `yaml:"numa_node,omitempty" validate:"numa_node:editable"`
	Config          map[string]interface{} `yaml:"config,omitempty" validate:"config:ignore"`
	ResourceControl meta.ResourceControl   `yaml:"resource_control,omitempty" validate:"resource_control:editable"`
	Arch            string                 `yaml:"arch,omitempty"`
	OS              string                 `yaml:"os,omitempty"`
}

// Role returns the component role of the instance
func (s *TikvProxySpec) Role() string {
	return ComponentTikvProxy
}

// SSH returns the host and SSH port of the instance
func (s *TikvProxySpec) SSH() (string, int) {
	return s.Host, s.SSHPort
}

// GetMainPort returns the main port of the instance
func (s *TikvProxySpec) GetMainPort() int {
	return s.Port
}

// IsImported returns if the node is imported from TiDB-Ansible
func (s *TikvProxySpec) IsImported() bool {
	return s.Imported
}

// TikvProxyComponent represents TiDB component.
type TikvProxyComponent struct{ Topology *Specification }

// Name implements Component interface.
func (c *TikvProxyComponent) Name() string {
	return ComponentTikvProxy
}

// Role implements Component interface.
func (c *TikvProxyComponent) Role() string {
	return ComponentTikvProxy
}

// Instances implements Component interface.
func (c *TikvProxyComponent) Instances() []Instance {
	ins := make([]Instance, 0, len(c.Topology.TikvProxyServers))
	for _, s := range c.Topology.TikvProxyServers {
		s := s
		ins = append(ins, &TikvProxyInstance{BaseInstance{
			InstanceSpec: s,
			Name:         c.Name(),
			Host:         s.Host,
			ListenHost:   s.ListenHost,
			Port:         s.Port,
			SSHP:         s.SSHPort,

			Ports: []int{
				s.Port,
			},
			Dirs: []string{
				s.DeployDir,
			},
			StatusFn: func(tlsCfg *tls.Config, _ ...string) string {
				return "-"
			},
		}, c.Topology})
	}
	return ins
}

// TikvProxyInstance represent the Tikv-Proxy instance
type TikvProxyInstance struct {
	BaseInstance
	topo Topology
}

// InitConfig implement Instance interface
func (i *TikvProxyInstance) InitConfig(
	ctx context.Context,
	e ctxt.Executor,
	clusterName,
	clusterVersion,
	deployUser string,
	paths meta.DirPaths,
) error {
	topo := i.topo.(*Specification)
	if err := i.BaseInstance.InitConfig(ctx, e, topo.GlobalOptions, deployUser, paths); err != nil {
		return err
	}

	enableTLS := topo.GlobalOptions.TLSEnabled
	spec := i.InstanceSpec.(*TikvProxySpec)
	cfg := scripts.NewTikvProxyScript(
		i.GetHost(),
		paths.Deploy,
		paths.Log,
	).WithPort(spec.Port). // listen port
				AppendEndpoints(topo.Endpoints(deployUser)...).
				WithListenHost(i.GetListenHost()) // listen host
	fp := filepath.Join(paths.Cache, fmt.Sprintf("run_proxy_%s_%d.sh", i.GetHost(), i.GetPort()))
	if err := cfg.ConfigToFile(fp); err != nil {
		return err
	}

	dst := filepath.Join(paths.Deploy, "scripts", "run_proxy.sh")
	if err := e.Transfer(ctx, fp, dst, false); err != nil {
		return err
	}
	if _, _, err := e.Execute(ctx, "chmod +x "+dst, false); err != nil {
		return err
	}

	globalConfig := topo.ServerConfigs.TikvProxy
	// merge config files for imported instance
	if i.IsImported() {
		configPath := ClusterPath(
			clusterName,
			AnsibleImportedConfigPath,
			fmt.Sprintf(
				"%s-%s-%d.toml",
				i.ComponentName(),
				i.GetHost(),
				i.GetPort(),
			),
		)
		importConfig, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		globalConfig, err = mergeImported(importConfig, globalConfig)
		if err != nil {
			return err
		}
	}

	// set TLS configs
	if enableTLS {
		if spec.Config == nil {
			spec.Config = make(map[string]interface{})
		}
		spec.Config["security.cluster-ssl-ca"] = fmt.Sprintf(
			"%s/tls/%s",
			paths.Deploy,
			TLSCACert,
		)
		spec.Config["security.cluster-ssl-cert"] = fmt.Sprintf(
			"%s/tls/%s.crt",
			paths.Deploy,
			i.Role())
		spec.Config["security.cluster-ssl-key"] = fmt.Sprintf(
			"%s/tls/%s.pem",
			paths.Deploy,
			i.Role())
	}

	if err := i.MergeServerConfig(ctx, e, globalConfig, spec.Config, paths); err != nil {
		return err
	}

	return checkConfig(ctx, e, i.ComponentName(), clusterVersion, i.OS(), i.Arch(), i.ComponentName()+".toml", paths, nil)
}

// ScaleConfig deploy temporary config on scaling
func (i *TikvProxyInstance) ScaleConfig(
	ctx context.Context,
	e ctxt.Executor,
	topo Topology,
	clusterName,
	clusterVersion,
	deployUser string,
	paths meta.DirPaths,
) error {
	s := i.topo
	defer func() { i.topo = s }()
	i.topo = mustBeTidbClusterTopo(topo)
	return i.InitConfig(ctx, e, clusterName, clusterVersion, deployUser, paths)
}

func mustBeTidbClusterTopo(topo Topology) *Specification {
	spec, ok := topo.(*Specification)
	if !ok {
		panic("must be cluster spec")
	}
	return spec
}
