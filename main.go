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

package main

import (
	"github.com/glassechidna/lastkeypair/cmd"
	"github.com/eawsy/aws-lambda-go-core/service/lambda/runtime"
	"encoding/json"
	"github.com/glassechidna/lastkeypair/common"
)

func main() {
	cmd.Execute()
}

func Handle(evt json.RawMessage, ctx *runtime.Context) (interface{}, error) {
	return common.LambdaHandle(evt, ctx)
}
