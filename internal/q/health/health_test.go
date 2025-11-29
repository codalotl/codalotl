package health

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func Test_writeAttrs(t *testing.T) {
	tests := []struct {
		name  string
		attrs []any
		want  string
	}{
		{
			name:  "empty",
			attrs: []any{},
			want:  "",
		},
		{
			name:  "simple pair",
			attrs: []any{"key", "value"},
			want:  `key=value`,
		},
		{
			name:  "multiple pairs",
			attrs: []any{"key1", "value1", "key2", 2, "key3", true},
			want:  `key1=value1 key2=2 key3=true`,
		},
		{
			name:  "slog.Attr",
			attrs: []any{slog.String("key", "value"), slog.Int("num", 123)},
			want:  `key=value num=123`,
		},
		{
			name:  "mixed",
			attrs: []any{"key1", "value1", slog.Bool("flag", false)},
			want:  `key1=value1 flag=false`,
		},
		{
			name:  "malformed",
			attrs: []any{"key_no_value"},
			want:  `!BADKEY=key_no_value`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeAttrs(&b, tt.attrs)
			if got := b.String(); got != tt.want {
				t.Errorf("writeAttrs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_healthErr_Error(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "message only",
			err:  NewErr("an error occurred"),
			want: "an error occurred",
		},
		{
			name: "message and attrs",
			err:  NewErr("file not found", "path", "/tmp/abc"),
			want: `file not found[path=/tmp/abc]`,
		},
		{
			name: "message and wrapped error",
			err:  Wrap("database error", errors.New("connection failed"), "db", "users"),
			want: `database error[db=users] via connection failed`,
		},
		{
			name: "wrapped health error",
			err:  Wrap("request failed", NewErr("auth failed", "user", "test"), "request_id", 123),
			want: `request failed[request_id=123] via auth failed[user=test]`,
		},
		{
			name: "deeply wrapped error",
			err:  Wrap("service layer", Wrap("repo layer", NewErr("db layer", "id", 1), "user", "jon"), "trace", "xyz"),
			want: `service layer[trace=xyz] via repo layer[user=jon] via db layer[id=1]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("healthErr.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLogErr(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	baseErr := errors.New("i am a base error")
	healthBaseErr := NewErr("i am a health error", "k", "v")

	tests := []struct {
		name    string
		logger  *slog.Logger
		err     error
		args    []any
		wantOut string
		wantErr error
	}{
		{
			name:    "nil logger",
			logger:  nil,
			err:     baseErr,
			wantErr: baseErr,
			wantOut: "",
		},
		{
			name:    "nil error",
			logger:  logger,
			err:     nil,
			wantErr: nil,
			wantOut: "",
		},
		{
			name:    "non-health error",
			logger:  logger,
			err:     baseErr,
			wantErr: baseErr,
			wantOut: `level=ERROR msg="i am a base error"`,
		},
		{
			name:    "non-health error with args",
			logger:  logger,
			err:     baseErr,
			args:    []any{"extra", "stuff"},
			wantErr: baseErr,
			wantOut: `level=ERROR msg="i am a base error" extra=stuff`,
		},
		{
			name:    "health error",
			logger:  logger,
			err:     healthBaseErr,
			wantErr: healthBaseErr,
			wantOut: `level=ERROR msg="i am a health error" k=v`,
		},
		{
			name:    "health error with args",
			logger:  logger,
			err:     healthBaseErr,
			args:    []any{"extra", "stuff"},
			wantErr: healthBaseErr,
			wantOut: `level=ERROR msg="i am a health error" k=v extra=stuff`,
		},
		{
			name:    "wrapped health error",
			logger:  logger,
			err:     Wrap("wrapper", baseErr, "wrapper_k", "wrapper_v"),
			wantErr: Wrap("wrapper", baseErr, "wrapper_k", "wrapper_v"),
			wantOut: `level=ERROR msg=wrapper wrapper_k=wrapper_v via="i am a base error"`,
		},
		{
			name:    "wrapped health error with args",
			logger:  logger,
			err:     Wrap("wrapper", healthBaseErr, "wrapper_k", "wrapper_v"),
			args:    []any{"extra", "stuff"},
			wantErr: Wrap("wrapper", healthBaseErr, "wrapper_k", "wrapper_v"),
			wantOut: `level=ERROR msg=wrapper wrapper_k=wrapper_v via="i am a health error[k=v]" extra=stuff`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			gotErr := LogErr(tt.logger, tt.err, tt.args...)
			if tt.wantErr == nil {
				if gotErr != nil {
					t.Errorf("LogErr() error = %v, wantErr nil", gotErr)
				}
			} else if gotErr == nil {
				t.Errorf("LogErr() error = nil, wantErr %v", tt.wantErr)
			} else if gotErr.Error() != tt.wantErr.Error() {
				t.Errorf("LogErr() error = %q, wantErr %q", gotErr.Error(), tt.wantErr.Error())
			}

			// Trim because slog may or may not add a final newline.
			gotOut := strings.TrimSpace(buf.String())

			// We don't care about the time key.
			if len(gotOut) > 0 {
				parts := strings.SplitN(gotOut, " ", 2)
				if strings.HasPrefix(parts[0], "time=") {
					gotOut = parts[1]
				}
			}

			if gotOut != tt.wantOut {
				t.Errorf("LogErr() gotOut = %q, want %q", gotOut, tt.wantOut)
			}
		})
	}
}

type myTestErr struct {
	msg string
	err error
}

func (e *myTestErr) Error() string {
	return e.msg
}

func (e *myTestErr) Unwrap() error {
	return e.err
}

func TestHealthErrWrapping(t *testing.T) {
	errSentinel := errors.New("sentinel")

	t.Run("errors.Is", func(t *testing.T) {
		// Chain: health.Wrap -> health.Wrap -> sentinel
		err := Wrap("layer 2", Wrap("layer 1", errSentinel))
		if !errors.Is(err, errSentinel) {
			t.Errorf("errors.Is failed: expected to find sentinel error in HealthErr chain")
		}

		// Chain: health.Wrap -> myTestErr -> sentinel
		err2 := Wrap("layer 2", &myTestErr{msg: "layer 1", err: errSentinel})
		if !errors.Is(err2, errSentinel) {
			t.Errorf("errors.Is failed: expected to find sentinel error in mixed chain")
		}
	})

	t.Run("errors.As", func(t *testing.T) {
		// Chain: health.Wrap -> myTestErr
		myErr := &myTestErr{msg: "my custom error"}
		err := Wrap("health error", myErr)

		var target *myTestErr
		if !errors.As(err, &target) {
			t.Fatalf("errors.As failed: expected to find myTestErr")
		}
		if target.msg != "my custom error" {
			t.Errorf("errors.As got wrong message: got %q, want %q", target.msg, "my custom error")
		}
		if target != myErr { // check same instance
			t.Errorf("errors.As got wrong instance")
		}
	})
}
