package cli

import (
	"encoding/json"

	"github.com/happyro/happyro-api/internal/catalog"
	"github.com/happyro/happyro-api/internal/config"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/cobra"
)

func catalogCommand() *cobra.Command {
	root := &cobra.Command{Use: "catalog", Short: "管理游戏资料快照", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	paths := catalog.Paths{}
	command := &cobra.Command{Use: "import", Short: "校验并原子发布完整的游戏资料", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return withAdmin(cmd.Context(), func(_ config.Config, db *sqlx.DB) error {
			service := &catalog.Importer{Reader: catalog.JSONReader{}, Publisher: &catalog.SQLPublisher{DB: db}}
			result, err := service.Import(cmd.Context(), paths)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		})
	}}
	command.Flags().StringVar(&paths.Client, "client-items", "", "客户端物品 JSON")
	command.Flags().StringVar(&paths.Server, "server-items", "", "Renewal 服务端物品 JSON")
	command.Flags().StringVar(&paths.Monsters, "monsters", "", "Renewal 魔物 JSON")
	command.Flags().StringVar(&paths.NPCs, "npcs", "", "NPC JSON")
	for _, flag := range []string{"client-items", "server-items", "monsters", "npcs"} {
		command.MarkFlagRequired(flag)
	}
	root.AddCommand(command, catalogExportCommand())
	return root
}
