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

package command

import (
  env "github.com/pingcap/tiup/pkg/environment"
  "github.com/pingcap/tiup/pkg/utils"
  "github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	offlineMode := false

	cmd := &cobra.Command{
		Use:   "update <cluster-name> <component1>[:version] [component2...N]",
		Short: "Separately update the tidb related components of the specified version",
		RunE: func(cmd *cobra.Command, args []string) error {

			if len(args) < 2 {
				return cmd.Help()
			}
      clusterName := args[0]
      teleCommand = append(teleCommand, scrubClusterName(clusterName))
      componmentVersionMap := make(map[string]utils.Version)
      for  i := 1;i < len(args);i++{
        teleCommand = append(teleCommand, args[i])
        component,version := env.ParseCompVersion(args[i])
        gOpt.Roles = append(gOpt.Roles, component)
        componmentVersionMap[component] = version
      }
			return cm.Update(clusterName,componmentVersionMap, gOpt, skipConfirm, offlineMode)
		},
	}
  cmd.Flags().StringSliceVarP(&gOpt.Nodes, "node", "N", nil, "Specify the nodes")
  // cmd.Flags().StringSliceVarP(&gOpt.Roles, "role", "R", nil, "Specify the roles")
	cmd.Flags().BoolVar(&gOpt.Force, "force", false, "Force upgrade without transferring PD leader")
	cmd.Flags().Uint64Var(&gOpt.APITimeout, "transfer-timeout", 300, "Timeout in seconds when transferring PD and TiKV store leaders")
	cmd.Flags().BoolVarP(&gOpt.IgnoreConfigCheck, "ignore-config-check", "", false, "Ignore the config check result")
	cmd.Flags().BoolVarP(&offlineMode, "offline", "", false, "Upgrade a stopped cluster")

	return cmd
}
