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

package manager

import (
	"context"
	"fmt"
  "github.com/pingcap/tiup/pkg/set"
  "os"

	"github.com/fatih/color"
	"github.com/joomcode/errorx"
	perrs "github.com/pingcap/errors"
	"github.com/pingcap/tiup/pkg/cliutil"
	"github.com/pingcap/tiup/pkg/cluster/clusterutil"
	"github.com/pingcap/tiup/pkg/cluster/ctxt"
	operator "github.com/pingcap/tiup/pkg/cluster/operation"
	"github.com/pingcap/tiup/pkg/cluster/spec"
	"github.com/pingcap/tiup/pkg/cluster/task"
	"github.com/pingcap/tiup/pkg/environment"
	"github.com/pingcap/tiup/pkg/logger/log"
	"github.com/pingcap/tiup/pkg/meta"
	"github.com/pingcap/tiup/pkg/repository"
	"github.com/pingcap/tiup/pkg/utils"
	"golang.org/x/mod/semver"
)

// Upgrade the cluster.
func (m *Manager) Update(name string, componentVersionMap map[string]utils.Version, opt operator.Options, skipConfirm, offline bool) error {
	if err := clusterutil.ValidateClusterNameOrError(name); err != nil {
		return err
	}

	metadata, err := m.meta(name)
	if err != nil {
		return err
	}

	topo := metadata.GetTopology()
	base := metadata.GetBaseMeta()

	var (
		downloadCompTasks []task.Task // tasks which are used to download components
		copyCompTasks     []task.Task // tasks which are used to copy components to remote host

		uniqueComps = map[string]struct{}{}
	)
  // add role and instance filter
  roleFilter := set.NewStringSet(opt.Roles...)
  nodeFilter := set.NewStringSet(opt.Nodes...)
  components := topo.ComponentsByUpdateOrder()
  components = operator.FilterComponent(components, roleFilter)

	//if err := updateVersionCompare(base.Version, clusterVersion); err != nil {
	//	return err
	//}

	if !skipConfirm {
		if err := cliutil.PromptForConfirmOrAbortError(
			"This operation will update %s %s cluster %s to %s.\nDo you want to continue? [y/N]:",
			m.sysName,
			color.HiYellowString(base.Version),
			color.HiYellowString(name),
			color.HiYellowString("UnKnow")); err != nil {
			return err
		}
		log.Infof("Upgrading cluster...")
	}

	hasImported := false
	for _, comp := range components {
    insts := operator.FilterInstance(comp.Instances(),nodeFilter)
		for _, inst := range insts {
			compName := inst.ComponentName()
			// update old version
			componentVersion := componentVersionMap[compName]
			inst.SetVersion(componentVersion.String())
			version := m.bindVersion(inst.ComponentName(), componentVersion.String())

			// Download component from repository
			key := fmt.Sprintf("%s-%s-%s-%s", compName, version, inst.OS(), inst.Arch())
			if _, found := uniqueComps[key]; !found {
				uniqueComps[key] = struct{}{}
				t := task.NewBuilder().
					Download(inst.ComponentName(), inst.OS(), inst.Arch(), version).
					Build()
				downloadCompTasks = append(downloadCompTasks, t)
			}

			deployDir := spec.Abs(base.User, inst.DeployDir())
			// data dir would be empty for components which don't need it
			dataDirs := spec.MultiDirAbs(base.User, inst.DataDir())
			// log dir will always be with values, but might not used by the component
			logDir := spec.Abs(base.User, inst.LogDir())

			// Deploy component
			tb := task.NewBuilder()
			if inst.IsImported() {
				switch inst.ComponentName() {
				case spec.ComponentPrometheus, spec.ComponentGrafana, spec.ComponentAlertmanager:
					tb.CopyComponent(
						inst.ComponentName(),
						inst.OS(),
						inst.Arch(),
						version,
						"", // use default srcPath
						inst.GetHost(),
						deployDir,
					)
				}
				hasImported = true
			}

			// backup files of the old version
			tb = tb.BackupComponent(inst.ComponentName(), inst.GetVersion(), inst.GetHost(), deployDir)

			if deployerInstance, ok := inst.(DeployerInstance); ok {
				deployerInstance.Deploy(tb, "", deployDir, version, name, inst.GetVersion())
			} else {
				// copy dependency component if needed
				switch inst.ComponentName() {
				case spec.ComponentTiSpark:
					env := environment.GlobalEnv()
					sparkVer, _, err := env.V1Repository().WithOptions(repository.Options{
						GOOS:   inst.OS(),
						GOARCH: inst.Arch(),
					}).LatestStableVersion(spec.ComponentSpark, false)
					if err != nil {
						return err
					}
					tb = tb.DeploySpark(inst, sparkVer.String(), "" /* default srcPath */, deployDir)
				default:
					tb = tb.CopyComponent(
						inst.ComponentName(),
						inst.OS(),
						inst.Arch(),
						version,
						"", // use default srcPath
						inst.GetHost(),
						deployDir,
					)
				}
			}

			tb.InitConfig(
				name,
				version,
				m.specManager,
				inst,
				base.User,
				opt.IgnoreConfigCheck,
				meta.DirPaths{
					Deploy: deployDir,
					Data:   dataDirs,
					Log:    logDir,
					Cache:  m.specManager.Path(name, spec.TempConfigPath),
				},
			)
			copyCompTasks = append(copyCompTasks, tb.Build())
			// update instance version
			inst.SetVersion(version)
		}
	}

	// handle dir scheme changes
	if hasImported {
		if err := spec.HandleImportPathMigration(name); err != nil {
			return err
		}
	}

	tlsCfg, err := topo.TLSConfig(m.specManager.Path(name, spec.TLSCertKeyDir))
	if err != nil {
		return err
	}
	t := m.sshTaskBuilder(name, topo, base.User, opt).
		Parallel(false, downloadCompTasks...).
		Parallel(opt.Force, copyCompTasks...).
		Func("UpgradeCluster", func(ctx context.Context) error {
			if offline {
				return nil
			}
			return operator.Upgrade(ctx, topo, opt, tlsCfg)
		}).
		Build()

	if err := t.Execute(ctxt.New(context.Background())); err != nil {
		if errorx.Cast(err) != nil {
			// FIXME: Map possible task errors and give suggestions.
			return err
		}
		return perrs.Trace(err)
	}

	// clear patched packages and tags
	if err := os.RemoveAll(m.specManager.Path(name, "patch")); err != nil {
		return perrs.Trace(err)
	}
	topo.IterInstance(func(ins spec.Instance) {
		if ins.IsPatched() {
			ins.SetPatched(false)
		}
	})

	if err := m.specManager.SaveMeta(name, metadata); err != nil {
		return err
	}

	log.Infof("Upgraded cluster `%s` successfully", name)

	return nil
}

func updateVersionCompare(curVersion, newVersion string) error {
	// Can always upgrade to 'nightly' event the current version is 'nightly'
	if newVersion == utils.NightlyVersionAlias {
		return nil
	}

	switch semver.Compare(curVersion, newVersion) {
	case -1:
		return nil
	case 0, 1:
		return perrs.Errorf("please specify a higher version than %s", curVersion)
	default:
		return perrs.Errorf("unreachable")
	}
}
