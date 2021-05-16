package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/cloudquery/cloudquery/internal/logging"
	"github.com/cloudquery/cloudquery/internal/signalcontext"
	"github.com/cloudquery/cloudquery/pkg/config"
	"github.com/cloudquery/cloudquery/pkg/ui"
	"github.com/cloudquery/cloudquery/pkg/ui/console"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var (
	initCmd = &cobra.Command{
		Use:   "init [choose one or more providers (aws,gcp,azure,okta,...)]",
		Short: "Generate initial config.hcl for fetch command",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			Initialize(args)
		},
	}
)

func Initialize(providers []string) {
	fs := afero.NewOsFs()
	if info, _ := fs.Stat("config.hcl"); info != nil {
		ui.ColorizedOutput(ui.ColorError, "Error: Config file already exists")
		return
	}
	f := hclwrite.NewEmptyFile()
	rootBody := f.Body()
	requiredProviders := make([]*config.RequiredProvider, len(providers))
	for i, p := range providers {
		requiredProviders[i] = &config.RequiredProvider{
			Name:    p,
			Source:  fmt.Sprintf("cloudquery/cq-provider-%s", p),
			Version: "latest",
		}
	}
	// TODO: build this manually with block and add comments as well
	cqBlock := gohcl.EncodeAsBlock(&config.CloudQuery{
		PluginDirectory: ".",
		Providers:       requiredProviders,
		Connection: config.Connection{
			DSN: "host=localhost user=postgres password=pass DB.name=postgres port=5432",
		},
	}, "cloudquery")

	rootBody.AppendBlock(cqBlock)
	cfg, diags := config.NewParser(nil).LoadConfigFromSource("init.hcl", f.Bytes())
	if diags != nil {
		fmt.Println(diags)
		return
	}

	ctx, _ := signalcontext.WithInterrupt(context.Background(), logging.NewZHcLog(&log.Logger, ""))
	c, err := console.CreateClientFromConfig(ctx, cfg)
	if err != nil {
		return
	}
	defer c.Client().Close()
	if err := c.DownloadProviders(ctx); err != nil {
		return
	}
	rootBody.AppendNewline()
	rootBody.AppendUnstructuredTokens(hclwrite.Tokens{
		{
			Type:  hclsyntax.TokenComment,
			Bytes: []byte("// All Provider Configurations"),
		},
	})
	rootBody.AppendNewline()
	var buffer bytes.Buffer
	buffer.WriteString("// Configuration AutoGenerated by CloudQuery CLI\n")
	if n, err := buffer.Write(f.Bytes()); n != len(f.Bytes()) || err != nil {
		fmt.Println(err)
		return
	}
	for _, p := range providers {
		pCfg, err := c.Client().GetProviderConfiguration(context.Background(), p)
		if err != nil {
			fmt.Println(err)
			return
		}
		buffer.Write(pCfg.Config)
		buffer.WriteString("\n")
	}
	formattedData := hclwrite.Format(buffer.Bytes())
	_ = afero.WriteFile(fs, "config.hcl", formattedData, os.ModePerm)
	ui.ColorizedOutput(ui.ColorSuccess, "configuration generated successfully to %s\n", "config.hcl")
}
