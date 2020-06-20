// Copyright 2016 Google Inc.
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

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.universe.tf/netboot/pixiecore"
)

var bootCmd = &cobra.Command{
	Use:   "boot kernel [initrd...]",
	Short: "Boot a kernel and optional init ramdisks",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			fatalf("you must specify at least a kernel")
		}
		kernel := args[0]
		initrds := args[1:]
		cmdline, err := cmd.Flags().GetString("cmdline")
		if err != nil {
			fatalf("Error reading flag: %s", err)
		}
		bootmsg, err := cmd.Flags().GetString("bootmsg")
		if err != nil {
			fatalf("Error reading flag: %s", err)
		}

		spec := &pixiecore.Spec{
			Kernel:  pixiecore.ID(kernel),
			Cmdline: cmdline,
			Message: bootmsg,
		}
		for _, initrd := range initrds {
			spec.Initrd = append(spec.Initrd, pixiecore.ID(initrd))
		}

		booter, err := pixiecore.StaticBooter(spec)
		if err != nil {
			fatalf("Couldn't make static booter: %s", err)
		}

		s := serverFromFlags(cmd)
		s.Booter = booter

		fmt.Println(s.Serve())
	},
}

func init() {
	rootCmd.AddCommand(bootCmd)
	serverConfigFlags(bootCmd)
	staticConfigFlags(bootCmd)
}
