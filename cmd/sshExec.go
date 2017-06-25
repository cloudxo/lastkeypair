// Copyright © 2017 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"github.com/spf13/cobra"
	"github.com/glassechidna/lastkeypair/common"
)

// sshExecCmd represents the sshExec command
var sshExecCmd = &cobra.Command{
	Use:   "exec",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		profile, _ := cmd.PersistentFlags().GetString("profile")
		region, _ := cmd.PersistentFlags().GetString("region")
		sess := common.AwsSession(profile, region)

		lambdaFunc, _ := cmd.PersistentFlags().GetString("lambda-func")
		kmsKeyId, _ := cmd.PersistentFlags().GetString("kms-key")
		funcIdentity, _ := cmd.PersistentFlags().GetString("func-identity")

		common.SshExec(sess, lambdaFunc, funcIdentity, kmsKeyId, args)
	},
}

func init() {
	sshCmd.AddCommand(sshExecCmd)

	sshExecCmd.PersistentFlags().String("lambda-func", "LastKeypair", "Function name or ARN")
	sshExecCmd.PersistentFlags().String("kms-key", "alias/LastKeypair", "ID, ARN or alias of KMS key for auth to CA")
	sshExecCmd.PersistentFlags().String("func-identity", "LastKeypair", "")
}
