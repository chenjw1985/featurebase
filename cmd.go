// Copyright 2022 Molecula Corp. (DBA FeatureBase).
// SPDX-License-Identifier: Apache-2.0
package pilosa

import (
	"io"

	"github.com/featurebasedb/featurebase/v3/logger"
)

// CmdIO holds standard unix inputs and outputs.
type CmdIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	logger logger.Logger
}

// NewCmdIO returns a new instance of CmdIO with inputs and outputs set to the
// arguments.
func NewCmdIO(stdin io.Reader, stdout, stderr io.Writer) *CmdIO {
	return &CmdIO{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		logger: logger.NewStandardLogger(stderr),
	}
}

func (c *CmdIO) Logger() logger.Logger {
	return c.logger
}
