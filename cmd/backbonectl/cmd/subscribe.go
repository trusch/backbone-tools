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
	"strings"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// subscribeCmd represents the listJobs command
var subscribeCmd = &cobra.Command{
	Use:   "subscribe",
	Short: "subscribe for events",
	Long:  `subscribe for events.`,
	Run: func(cmd *cobra.Command, args []string) {
		cli := api.NewEventsClient(grpcConnection)
		topic, _ := cmd.Flags().GetString("topic")
		labels, _ := cmd.Flags().GetStringSlice("label")
		labelMap := parseLabels(labels)
		sinceSeq, _ := cmd.Flags().GetUint64("since-sequence")
		sinceStr, _ := cmd.Flags().GetString("since")
		var sinceProtoTime timestamp.Timestamp
		if sinceStr != "" {
			err := jsonpb.Unmarshal(strings.NewReader(fmt.Sprintf(`"%s"`, sinceStr)), &sinceProtoTime)
			if err != nil {
				logrus.Fatal(errors.Wrap(err, "failed parsing --since parameter"))
			}
		}
		resp, err := cli.Subscribe(context.Background(), &api.SubscribeRequest{
			Topic:          topic,
			Labels:         labelMap,
			SinceCreatedAt: &sinceProtoTime,
			SinceSequence:  sinceSeq,
		})
		if err != nil {
			logrus.Fatal(err)
		}
		marshaler := jsonpb.Marshaler{
			Indent: "  ",
		}
		for {
			event, err := resp.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				logrus.Fatal(err)
			}
			err = marshaler.Marshal(os.Stdout, event)
			if err != nil {
				logrus.Fatal(err)
			}
			fmt.Println("")
		}
	},
}

func init() {
	eventsCmd.AddCommand(subscribeCmd)
	subscribeCmd.Flags().String("topic", "", "topic to listen on")
	subscribeCmd.Flags().String("since", "", "only show events newer than this")
	subscribeCmd.Flags().Uint64("since-sequence", 0, "only show events newer than this")
	subscribeCmd.Flags().StringSlice("label", []string{}, "labels used to filter")
}
