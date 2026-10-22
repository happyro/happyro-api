package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/happyro/happyro-api/internal/catalog"
	"github.com/spf13/cobra"
)

func catalogExportCommand() *cobra.Command {
	var source, output string
	var check bool
	command := &cobra.Command{Use: "export-subtypes", Short: "生成客户端物品子类与装备职业映射", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		raw, err := catalog.ExportSubtypes(source)
		if err != nil {
			return err
		}
		if check {
			existing, err := os.ReadFile(output)
			if err != nil {
				return err
			}
			if !bytes.Equal(raw, existing) {
				return errors.New("物品子类映射已过期，请重新生成。")
			}
			return nil
		}
		file, err := os.CreateTemp(filepath.Dir(output), ".item-subtypes-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err = file.Write(raw); err != nil {
			file.Close()
			return err
		}
		if err = file.Chmod(0644); err != nil {
			file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
		return os.Rename(file.Name(), output)
	}}
	command.Flags().StringVar(&source, "server-items", "", "Renewal 服务端物品 JSON")
	command.Flags().StringVar(&output, "output", "", "生成文件路径")
	command.Flags().BoolVar(&check, "check", false, "仅检查生成文件是否为最新")
	command.MarkFlagRequired("server-items")
	command.MarkFlagRequired("output")
	return command
}
