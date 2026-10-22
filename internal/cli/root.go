package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/happyro/happyro-api/internal/app"
	"github.com/happyro/happyro-api/internal/config"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/cobra"
	"io"
	"os"
	"strings"
	"time"
)

func NewRoot() *cobra.Command {
	var noColor bool
	root := &cobra.Command{Use: "happyro-api", Short: "HappyRO API", SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "禁用 ANSI 颜色")
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		style := func(code, text string) string {
			if noColor {
				return text
			}
			return "\033[" + code + "m" + text + "\033[0m"
		}
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "\n"+style("1;36", cmd.Short)+"\n")
		fmt.Fprintln(out, style("1;33", "用法"))
		fmt.Fprintln(out, "  "+style("1;32", cmd.UseLine()))
		if cmd.HasAvailableSubCommands() {
			fmt.Fprintln(out, "\n"+style("1;33", "子命令"))
			for _, child := range cmd.Commands() {
				if child.IsAvailableCommand() {
					fmt.Fprintf(out, "  %s  %s\n", style("1;32", child.Name()), child.Short)
				}
			}
		}
		fmt.Fprintln(out, "\n"+style("1;33", "选项"))
		fmt.Fprint(out, cmd.LocalFlags().FlagUsages())
		fmt.Fprint(out, cmd.InheritedFlags().FlagUsages())
		fmt.Fprintln(out, "\n"+style("1;33", "常用例子"))
		for _, example := range []string{"happyro-api migrate", "happyro-api init", "happyro-api serve", "happyro-api catalog import --client-items client.json --server-items items.json --monsters monsters.json --npcs npcs.json", "happyro-api users create operator --role operator --password-stdin", "happyro-api users password admin --password-stdin"} {
			fmt.Fprintln(out, style("36", "  "+example))
		}
		fmt.Fprintln(out)
	})
	root.AddCommand(&cobra.Command{Use: "serve", Short: "启动 HTTP 服务", Args: cobra.NoArgs, RunE: serve})
	root.AddCommand(&cobra.Command{Use: "migrate", Short: "迁移管理库结构", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return withAdmin(cmd.Context(), func(_ config.Config, db *sqlx.DB) error { return database.Migrate(cmd.Context(), db) })
	}})
	root.AddCommand(&cobra.Command{Use: "init", Short: "仅在管理库为空时创建初始管理员", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return withIdentity(cmd.Context(), func(cfg config.Config, service *identityapi.Service) error {
			created, err := service.Provision(cmd.Context(), identityapi.NewUser{Username: cfg.InitialUsername, Password: cfg.InitialPassword, Role: "super_admin"}, true)
			if err != nil {
				return err
			}
			if created {
				cmd.Println("初始管理员已创建。")
			} else {
				cmd.Println("管理库已有账号，跳过初始管理员创建。")
			}
			return nil
		})
	}})
	root.AddCommand(usersCommand(), catalogCommand())
	return root
}

func withAdmin(ctx context.Context, run func(config.Config, *sqlx.DB) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	connect, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := database.Open(connect, cfg.AdminDB)
	if err != nil {
		return err
	}
	defer db.Close()
	return run(cfg, db)
}
func withIdentity(ctx context.Context, run func(config.Config, *identityapi.Service) error) error {
	return withAdmin(ctx, func(cfg config.Config, db *sqlx.DB) error {
		service := identityapi.NewService(&identityapi.SQLRepository{DB: db}, identityapi.Argon2id{})
		return run(cfg, service)
	})
}

func usersCommand() *cobra.Command {
	users := &cobra.Command{Use: "users", Short: "管理后台账号", RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	var role, name string
	var createStdin, changeStdin bool
	create := &cobra.Command{Use: "create <username>", Short: "创建管理账号", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		password, err := readPassword(cmd, createStdin)
		if err != nil {
			return err
		}
		return withIdentity(cmd.Context(), func(_ config.Config, service *identityapi.Service) error {
			_, err := service.Provision(cmd.Context(), identityapi.NewUser{Username: args[0], Name: name, Password: password, Role: role}, false)
			if err == nil {
				cmd.Println("管理账号已创建。")
			}
			return err
		})
	}}
	create.Flags().StringVar(&role, "role", "super_admin", "super_admin、administrator、operator 或 auditor")
	create.Flags().StringVar(&name, "name", "", "显示名称")
	create.Flags().BoolVar(&createStdin, "password-stdin", false, "从标准输入读取一行密码")
	change := &cobra.Command{Use: "password <username>", Short: "修改密码并使现有登录失效", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		password, err := readPassword(cmd, changeStdin)
		if err != nil {
			return err
		}
		return withIdentity(cmd.Context(), func(_ config.Config, service *identityapi.Service) error {
			err := service.ChangePassword(cmd.Context(), args[0], password)
			if err == nil {
				cmd.Println("密码已更新，现有登录将在下次请求时失效。")
			}
			return err
		})
	}}
	change.Flags().BoolVar(&changeStdin, "password-stdin", false, "从标准输入读取一行密码")
	users.AddCommand(create, change)
	return users
}

func readPassword(cmd *cobra.Command, stdin bool) (string, error) {
	if !stdin {
		return "", errors.New("请使用 --password-stdin 从标准输入提供密码")
	}
	reader := bufio.NewReader(io.LimitReader(cmd.InOrStdin(), 258))
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value = strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r")
	if len(value) > 255 {
		return "", errors.New("密码最多 255 个字节。")
	}
	return value, nil
}

func serve(cmd *cobra.Command, _ []string) error {
	return withAdmin(cmd.Context(), func(cfg config.Config, db *sqlx.DB) error { return app.Serve(cmd.Context(), cfg, db) })
}

func Execute(ctx context.Context) int {
	if err := NewRoot().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
