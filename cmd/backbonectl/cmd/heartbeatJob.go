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
	"os"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// heartbeatCmd represents the deleteJobs command
var heartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "send a heartbeat for a job",
	Long:  `send a heartbeat for a job.`,
	Run: func(cmd *cobra.Command, args []string) {
		id, _ := cmd.Flags().GetString("id")
		status, _ := cmd.Flags().GetString("status")
		finished, _ := cmd.Flags().GetBool("finished")
		cli := api.NewJobsClient(grpcConnection)
		job, err := cli.Heartbeat(context.Background(), &api.HeartbeatRequest{
			JobId:    id,
			State:    []byte(status),
			Finished: finished,
		})
		if err != nil {
			logrus.Fatal(err)
		}
		marshaler := jsonpb.Marshaler{
			Indent: "  ",
		}
		err = marshaler.Marshal(os.Stdout, job)
		if err != nil {
			logrus.Fatal(err)
		}
		fmt.Println("")
	},
}

func init() {
	jobsCmd.AddCommand(heartbeatCmd)
	heartbeatCmd.Flags().String("id", "", "id of the job")
	heartbeatCmd.Flags().String("status", "", "updated status object to submit")
	heartbeatCmd.Flags().Bool("finished", false, "finished flag")

}
