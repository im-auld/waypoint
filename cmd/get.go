// Copyright © 2018 NAME HERE <EMAIL ADDRESS>
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
	"fmt"

	"os"

	"github.com/spf13/cobra"
)

// getCmd represents the get command
var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Get all versions for an cmd",
	Long:  `The get command will list out all available versions for an cmd in yaml format.`,
	Run: func(cmd *cobra.Command, args []string) {
		versions, err := storage.All(conf.App)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(2)
		}
		listAll(versions)
	},
}

func init() {
	getCmd.Flags().Bool("all", false, "a bool")
	rootCmd.AddCommand(getCmd)
}
