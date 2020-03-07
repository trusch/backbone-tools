/*
Copyright © 2020 Tino Rusch <tino.rusch@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"context"
	"os"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// aquireCmd represents the aquire command
var aquireCmd = &cobra.Command{
	Use:   "aquire",
	Short: "aquire a lock",
	Long:  `aquire a lock.`,
	Run: func(cmd *cobra.Command, args []string) {
		cli := api.NewLocksClient(grpcConnection)
		id, _ := cmd.Flags().GetString("id")
		lock, err := cli.Aquire(context.Background(), &api.AquireRequest{
			Id: id,
		})
		if err != nil {
			logrus.Fatal(err)
		}
		marshaler := jsonpb.Marshaler{
			Indent: "  ",
		}
		err = marshaler.Marshal(os.Stdout, lock)
		if err != nil {
			logrus.Fatal(errors.Wrap(err, "failed to marshal lock"))
		}
	},
}

func init() {
	locksCmd.AddCommand(aquireCmd)
	aquireCmd.Flags().String("id", "", "lock id to aquire")
}
