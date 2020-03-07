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
	"strings"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// createJobCmd represents the createJob command
var createJobCmd = &cobra.Command{
	Use:   "create",
	Short: "create a job",
	Long:  `create a job.`,
	Run: func(cmd *cobra.Command, args []string) {
		cli := api.NewJobsClient(grpcConnection)
		queue, _ := cmd.Flags().GetString("queue")
		spec, _ := cmd.Flags().GetString("spec")
		labels, _ := cmd.Flags().GetStringSlice("label")
		labelMap := parseLabels(labels)
		job, err := cli.Create(context.Background(), &api.CreateJobRequest{
			Queue:  queue,
			Spec:   []byte(spec),
			Labels: labelMap,
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
	},
}

func init() {
	jobsCmd.AddCommand(createJobCmd)
	createJobCmd.Flags().String("queue", "", "where to put the job in")
	createJobCmd.Flags().String("spec", "", "job specification")
	createJobCmd.Flags().StringSlice("label", []string{}, "job labels")
}

func parseLabels(labels []string) map[string]string {
	labelMap := make(map[string]string)
	for _, labelPair := range labels {
		parts := strings.Split(labelPair, "=")
		if len(parts) != 2 {
			logrus.Fatal("format your labels correctly.")
		}
		labelMap[parts[0]] = parts[1]
	}
	return labelMap
}
