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

package scripts

import (
	"bytes"
	"os"
	"path"
	"text/template"

	"github.com/pingcap/tiup/embed"
)

// TikvProxyScript represent the data to generate Tikv-Proxy config
type TikvProxyScript struct {
	Name       string
	IP         string
	ListenHost string
	Port       int
	DeployDir  string
	LogDir     string
	NumaNode   string
	Endpoints  []*PDScript
}

// NewTikvProxyScript returns a TikvProxyScript with given arguments
func NewTikvProxyScript(ip, deployDir, logDir string) *TikvProxyScript {
	return &TikvProxyScript{
		IP:        ip,
		Port:      21000,
		DeployDir: deployDir,
		LogDir:    logDir,
	}
}

// WithListenHost set ListenHost field of TikvProxyScript
func (c *TikvProxyScript) WithListenHost(listenHost string) *TikvProxyScript {
	c.ListenHost = listenHost
	return c
}

// WithPort set Port field of TikvProxyScript
func (c *TikvProxyScript) WithPort(port int) *TikvProxyScript {
	c.Port = port
	return c
}

// WithNumaNode set NumaNode field of TiKVScript
func (c *TikvProxyScript) WithNumaNode(numa string) *TikvProxyScript {
	c.NumaNode = numa
	return c
}

// AppendEndpoints add new PDScript to Endpoints field
func (c *TikvProxyScript) AppendEndpoints(ends ...*PDScript) *TikvProxyScript {
	c.Endpoints = append(c.Endpoints, ends...)
	return c
}

// Config generate the config file data.
func (c *TikvProxyScript) Config() ([]byte, error) {
	fp := path.Join("templates", "scripts", "run_proxy.sh.tpl")
	tpl, err := embed.ReadFile(fp)
	if err != nil {
		return nil, err
	}
	return c.ConfigWithTemplate(string(tpl))
}

// ConfigToFile write config content to specific path
func (c *TikvProxyScript) ConfigToFile(file string) error {
	config, err := c.Config()
	if err != nil {
		return err
	}
	return os.WriteFile(file, config, 0755)
}

// ConfigWithTemplate generate the TikvProxy config content by tpl
func (c *TikvProxyScript) ConfigWithTemplate(tpl string) ([]byte, error) {
	tmpl, err := template.New("TikvProxy").Parse(tpl)
	if err != nil {
		return nil, err
	}

	content := bytes.NewBufferString("")
	if err := tmpl.Execute(content, c); err != nil {
		return nil, err
	}

	return content.Bytes(), nil
}
