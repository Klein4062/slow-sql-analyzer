package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Klein4062/slow-sql-analyzer/internal/plan"
)

// CommandSource runs an external command to obtain an EXPLAIN (FORMAT JSON)
// document, instead of connecting via the built-in pgx driver.
//
// This is the "bring your own client" connector: the command (psql, a bastion
// wrapper, ssh, a custom script, …) must print the EXPLAIN JSON (top-level
// array) to stdout. Placeholders {dsn} and {sql} in Cmd are substituted with
// the connection string and query; the same values are also exposed via the
// SSA_DSN / SSA_SQL / SSA_TIMEOUT environment variables so templates can avoid
// shell-quoting pitfalls.
//
// 自定义客户端连接器：用模板命令自己跑 EXPLAIN，工具只解析其 stdout 的 JSON。
// 适合内网受限环境（堡垒机、包装脚本、仅 psql 可用等）。
// 注意：安全控制（只读事务、statement_timeout、写语句拦截）由你的命令自行负责。
type CommandSource struct {
	Cmd     string        // 命令模板，支持 {dsn} / {sql} 占位符
	DSN     string        // 连接串（注入到 {dsn} 与 SSA_DSN；命令可忽略）
	Query   string        // SQL（注入到 {sql} 与 SSA_SQL）
	Timeout time.Duration // 子进程超时（同时写入 SSA_TIMEOUT）
}

// Fetch implements PlanSource.
func (s CommandSource) Fetch() (*plan.PlanResult, error) {
	if strings.TrimSpace(s.Query) == "" {
		return nil, errors.New("no query to analyze")
	}
	if strings.TrimSpace(s.Cmd) == "" {
		return nil, errors.New("--exec command is empty")
	}

	// 占位符替换。注：直接原样替换进命令，SQL/DSN 含特殊字符时建议改用 $SSA_* 环境变量。
	cmdLine := strings.ReplaceAll(s.Cmd, "{dsn}", s.DSN)
	cmdLine = strings.ReplaceAll(cmdLine, "{sql}", s.Query)

	timeout := s.timeoutOr()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 跨平台选择 shell：unix 用 sh -c，windows 用 cmd /c（支持管道/重定向/ssh 等）。
	shell, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/c"
	}
	c := exec.CommandContext(ctx, shell, flag, cmdLine)

	// 始终把连接串/SQL/超时通过环境变量传出，模板可用 $SSA_DSN / $SSA_SQL / $SSA_TIMEOUT。
	c.Env = append(os.Environ(),
		"SSA_DSN="+s.DSN,
		"SSA_SQL="+s.Query,
		fmt.Sprintf("SSA_TIMEOUT=%dms", int(timeout/time.Millisecond)),
	)

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("exec connector timed out after %s", timeout)
		}
		return nil, fmt.Errorf("exec connector failed: %w; stderr: %s", err, strings.TrimSpace(stderr.String()))
	}

	result, err := plan.Parse(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse command stdout as EXPLAIN (FORMAT JSON): %w", err)
	}
	result.SourceQuery = s.Query
	return result, nil
}

func (s CommandSource) timeoutOr() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return 30 * time.Second
}
