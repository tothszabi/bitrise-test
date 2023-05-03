package stepoutput

import (
	"io"

	"github.com/tothszabi/bitrise-test/log"
	"github.com/tothszabi/bitrise-test/log/logwriter"
	"github.com/tothszabi/bitrise-test/tools/errorfinder"
	"github.com/tothszabi/bitrise-test/tools/filterwriter"
)

type Writer struct {
	writer io.Writer

	secretWriter *filterwriter.Writer
	errorWriter  *errorfinder.ErrorFinder
	logWriter    *logwriter.LogWriter
}

func NewWriter(secrets []string, opts log.LoggerOpts) Writer {
	var outWriter io.Writer

	logWriter := logwriter.NewLogWriter(log.NewLogger(opts))
	outWriter = logWriter

	errorWriter := errorfinder.NewErrorFinder(outWriter, opts.TimeProvider)
	outWriter = errorWriter

	var secretWriter *filterwriter.Writer
	if len(secrets) > 0 {
		secretWriter = filterwriter.New(secrets, outWriter)
		outWriter = secretWriter
	}

	return Writer{
		writer: outWriter,

		secretWriter: secretWriter,
		errorWriter:  errorWriter,
		logWriter:    logWriter,
	}
}

func (w Writer) Write(p []byte) (n int, err error) {
	return w.writer.Write(p)
}

func (w Writer) Close() error {
	if w.secretWriter != nil {
		if err := w.secretWriter.Close(); err != nil {
			return err
		}
	}

	if err := w.errorWriter.Close(); err != nil {
		return err
	}

	return w.logWriter.Close()
}

func (w Writer) ErrorMessages() []string {
	return w.errorWriter.ErrorMessages()
}
