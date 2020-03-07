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
	"fmt"
	"io"
	"os"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// listenJobsCmd represents the listJobs command
var listenJobsCmd = &cobra.Command{
	Use:   "listen",
	Short: "listen for jobs",
	Long:  `listen for jobs.`,
	Run: func(cmd *cobra.Command, args []string) {
		queue, _ := cmd.Flags().GetString("queue")
		cli := api.NewJobsClient(grpcConnection)
		resp, err := cli.Listen(context.Background(), &api.ListenRequest{
			Queue: queue,
		})
		if err != nil {
			logrus.Fatal(err)
		}
		marshaler := jsonpb.Marshaler{
			Indent: "  ",
		}
		for {
			job, err := resp.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				logrus.Fatal(err)
			}
			err = marshaler.Marshal(os.Stdout, job)
			if err != nil {
				logrus.Fatal(err)
			}
			fmt.Println("")
		}
	},
}

func init() {
	jobsCmd.AddCommand(listenJobsCmd)
	listenJobsCmd.Flags().String("queue", "", "queue to listen on")
}
