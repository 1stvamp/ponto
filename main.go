package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tfe "github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"
	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// version is the build version; goreleaser overrides it via -ldflags at
// release time (see .goreleaser.yml), so the tag drives the reported version.
var version = "1.1.0"

var TRUE = true

//go:embed ui/dist
var frontend embed.FS

type ponto struct {
	Name             string
	WorkingDir       string
	TfPath           string
	TfVarsFiles      []string
	TfVars           []string
	TfBackendConfigs []string
	PlanPath         string
	PlanJSONPath     string
	WorkspaceName    string
	TFCOrgName       string
	TFCWorkspaceName string
	ShowSensitive    bool
	ImageFormat      string
	Output           string
	Verbose          bool
	TFCNewRun        bool
	Plan             *tfjson.Plan
	RSO              *ResourcesOverview
	Map              *Map
	Graph            Graph
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// resolveTfPath returns the terraform binary to use. An explicit --tf-path
// wins; otherwise look up terraform then tofu on $PATH, falling back to the
// container's /bin/terraform.
func resolveTfPath(explicit, workingDir string) string {
	if explicit != "" {
		return explicit
	}
	// A version-manager file (tfenv/tofuenv/asdf/mise) tells us whether the
	// project is terraform or tofu; honour that, then fall back to the usual
	// order. Either way the binary itself still comes from $PATH (which is
	// where the version manager's shim lives).
	order := []string{"terraform", "tofu"}
	if preferredTfTool(workingDir) == "tofu" {
		order = []string{"tofu", "terraform"}
	}
	for _, bin := range order {
		if p, err := exec.LookPath(bin); err == nil {
			return p
		}
	}
	return "/bin/terraform"
}

// preferredTfTool returns "terraform" or "tofu" when a version-manager file in
// workingDir, one of its parents, or $HOME says which the project uses. Empty
// if nothing is found. Mirrors how tfenv/tofuenv/tenv search for their files.
func preferredTfTool(workingDir string) string {
	dir, err := filepath.Abs(workingDir)
	if err != nil {
		dir = workingDir
	}
	dirs := []string{}
	for {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, home)
	}
	for _, d := range dirs {
		if tool := tfToolFromDir(d); tool != "" {
			return tool
		}
	}
	return ""
}

func tfToolFromDir(dir string) string {
	// tofuenv / tfenv single-purpose files first.
	if fileExists(filepath.Join(dir, ".opentofu-version")) {
		return "tofu"
	}
	if fileExists(filepath.Join(dir, ".terraform-version")) || fileExists(filepath.Join(dir, ".tfswitchrc")) {
		return "terraform"
	}
	// asdf / mise .tool-versions: a terraform or opentofu line.
	if b, err := os.ReadFile(filepath.Join(dir, ".tool-versions")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			switch fields := strings.Fields(line); {
			case len(fields) == 0:
			case fields[0] == "opentofu" || fields[0] == "tofu":
				return "tofu"
			case fields[0] == "terraform":
				return "terraform"
			}
		}
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func newRootCmd() *cobra.Command {
	modeFlags := pflag.NewFlagSet("mode", pflag.ContinueOnError)
	modeFlags.BoolP("standalone", "s", false, "Deprecated: static HTML output is now the default")
	modeFlags.BoolP("gen-image", "g", false, "Generate a graph image (svg/png)")
	modeFlags.BoolP("tui", "i", false, "Launch the interactive terminal UI (alias: --interactive)")

	inputFlags := pflag.NewFlagSet("input", pflag.ContinueOnError)
	inputFlags.StringP("working-dir", "C", ".", "Path to the Terraform configuration")
	inputFlags.StringP("plan-path", "p", "", "Path to a pre-generated binary plan file")
	inputFlags.StringP("plan-json-path", "j", "", "Path to a pre-generated JSON plan file")
	inputFlags.StringArrayP("tf-vars-file", "f", nil, "Path to a *.tfvars file (repeatable)")
	inputFlags.StringArray("tf-var", nil, "Terraform variable, key=value (repeatable)")
	inputFlags.StringArray("tf-backend-config", nil, "Path to a *.tfbackend file (repeatable)")
	inputFlags.StringP("tf-path", "t", "", "Path to the terraform/tofu binary (default: terraform, then tofu, then /bin/terraform)")
	inputFlags.StringP("workspace", "w", "", "Terraform workspace name")
	inputFlags.String("tfc-org", "", "Terraform Cloud organization name")
	inputFlags.String("tfc-workspace", "", "Terraform Cloud workspace name")
	inputFlags.Bool("tfc-new-run", false, "Create a new Terraform Cloud run")

	outputFlags := pflag.NewFlagSet("output", pflag.ContinueOnError)
	outputFlags.StringP("output", "o", "ponto", "Base name for generated files (.zip/.svg/.png)")
	outputFlags.Bool("open", false, "Open the generated HTML in your browser")
	outputFlags.String("image-format", "svg", "Image format for --gen-image: svg or png")
	outputFlags.String("name", "ponto", "Configuration name")
	outputFlags.Bool("show-sensitive", false, "Display sensitive values")

	metaFlags := pflag.NewFlagSet("meta", pflag.ContinueOnError)
	metaFlags.BoolP("verbose", "v", false, "Verbose logging (stream terraform output)")

	v := viper.New()

	cmd := &cobra.Command{
		Use:   "ponto",
		Short: "Ponto is an interactive Terraform plan and state visualizer",
		Long: "Ponto renders a Terraform plan or state as an interactive graph.\n" +
			"By default it writes a single self-contained HTML file; it can also emit\n" +
			"a static image or an interactive terminal UI.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, v)
		},
	}
	cmd.SetVersionTemplate("Ponto v{{.Version}}\n")

	cmd.Flags().AddFlagSet(modeFlags)
	cmd.Flags().AddFlagSet(inputFlags)
	cmd.Flags().AddFlagSet(outputFlags)
	cmd.Flags().AddFlagSet(metaFlags)
	cmd.MarkFlagsMutuallyExclusive("standalone", "gen-image", "tui")

	cmd.AddCommand(newSummaryCmd())

	// Allow --interactive as an alias for --tui.
	cmd.Flags().SetNormalizeFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "interactive" {
			name = "tui"
		}
		return pflag.NormalizedName(name)
	})

	v.SetEnvPrefix("PONTO")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	v.BindPFlags(cmd.Flags())

	cmd.SetUsageFunc(func(c *cobra.Command) error {
		out := c.OutOrStderr()
		fmt.Fprintf(out, "Usage:\n  %s [flags]\n\n", c.CommandPath())
		fmt.Fprintf(out, "Modes (default: write a self-contained HTML file):\n%s\n", modeFlags.FlagUsages())
		fmt.Fprintf(out, "Input:\n%s\n", inputFlags.FlagUsages())
		fmt.Fprintf(out, "Output:\n%s\n", outputFlags.FlagUsages())
		fmt.Fprintf(out, "Other:\n%s", metaFlags.FlagUsages())
		fmt.Fprintf(out, "  -h, --help      Show this help\n")
		fmt.Fprintf(out, "      --version   Print the version\n")
		return nil
	})

	return cmd
}

// buildPonto constructs the ponto config from the resolved flags/env, shared by
// the root command and the summary subcommand.
func buildPonto(cmd *cobra.Command, v *viper.Viper) (ponto, error) {
	tfVarsFiles, _ := cmd.Flags().GetStringArray("tf-vars-file")
	tfVars, _ := cmd.Flags().GetStringArray("tf-var")
	tfBackendConfigs, _ := cmd.Flags().GetStringArray("tf-backend-config")

	path, err := os.Getwd()
	if err != nil {
		return ponto{}, errors.New("unable to get current working directory")
	}

	planPath := v.GetString("plan-path")
	if planPath != "" && !strings.HasPrefix(planPath, "/") {
		planPath = filepath.Join(path, planPath)
	}
	planJSONPath := v.GetString("plan-json-path")
	if planJSONPath != "" && !strings.HasPrefix(planJSONPath, "/") {
		planJSONPath = filepath.Join(path, planJSONPath)
	}

	return ponto{
		Name:             v.GetString("name"),
		WorkingDir:       v.GetString("working-dir"),
		TfPath:           resolveTfPath(v.GetString("tf-path"), v.GetString("working-dir")),
		PlanPath:         planPath,
		PlanJSONPath:     planJSONPath,
		ShowSensitive:    v.GetBool("show-sensitive"),
		Output:           v.GetString("output"),
		Verbose:          v.GetBool("verbose"),
		TfVarsFiles:      tfVarsFiles,
		TfVars:           tfVars,
		TfBackendConfigs: tfBackendConfigs,
		WorkspaceName:    v.GetString("workspace"),
		TFCOrgName:       v.GetString("tfc-org"),
		TFCWorkspaceName: v.GetString("tfc-workspace"),
		TFCNewRun:        v.GetBool("tfc-new-run"),
	}, nil
}

// newSummaryCmd is the `ponto summary` subcommand: a safety-graded plan digest.
func newSummaryCmd() *cobra.Command {
	inputFlags := pflag.NewFlagSet("input", pflag.ContinueOnError)
	inputFlags.StringP("working-dir", "C", ".", "Path to the Terraform configuration")
	inputFlags.StringP("plan-path", "p", "", "Path to a pre-generated binary plan file")
	inputFlags.StringP("plan-json-path", "j", "", "Path to a pre-generated JSON plan file")
	inputFlags.StringArrayP("tf-vars-file", "f", nil, "Path to a *.tfvars file (repeatable)")
	inputFlags.StringArray("tf-var", nil, "Terraform variable, key=value (repeatable)")
	inputFlags.StringArray("tf-backend-config", nil, "Path to a *.tfbackend file (repeatable)")
	inputFlags.StringP("tf-path", "t", "", "Path to the terraform/tofu binary")
	inputFlags.StringP("workspace", "w", "", "Terraform workspace name")
	inputFlags.Bool("show-sensitive", false, "Display sensitive values")
	inputFlags.BoolP("verbose", "v", false, "Verbose logging (stream terraform output)")

	sumFlags := pflag.NewFlagSet("summary", pflag.ContinueOnError)
	sumFlags.String("format", "terminal", "Output format: terminal, markdown, image or tui")
	sumFlags.String("emoji", "dots", "Emoji encoding: dots, signs or none")
	sumFlags.StringP("output", "o", "ponto-summary", "Base name for the image card PNG (--format image)")
	sumFlags.String("image-format", "png", "Image format for --format image: png or svg")
	sumFlags.BoolP("interactive", "i", false, "Explore the terminal summary interactively (alias: --format tui)")

	v := viper.New()

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Print a safety-graded plan digest (Safe/Caution/Danger)",
		Long: "Classify a Terraform plan into Safe / Caution / Danger tiers and render it\n" +
			"as a terminal summary, a markdown PR comment, or an image card. Exits 2 when\n" +
			"the plan is destructive, so it drops into CI as a change gate.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildPonto(cmd, v)
			if err != nil {
				return err
			}
			imageFormat := v.GetString("image-format")
			if v.GetString("format") == "image" && imageFormat != "png" && imageFormat != "svg" {
				return fmt.Errorf("invalid --image-format %q: must be \"png\" or \"svg\"", imageFormat)
			}
			code, err := runSummary(&r, v.GetString("format"), v.GetString("emoji"), v.GetString("output"), imageFormat, v.GetBool("interactive"))
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}

	cmd.Flags().AddFlagSet(inputFlags)
	cmd.Flags().AddFlagSet(sumFlags)

	// The root command sets a custom usage func that cobra otherwise propagates
	// to subcommands; give summary its own so it lists its own flags.
	cmd.SetUsageFunc(func(c *cobra.Command) error {
		out := c.OutOrStderr()
		fmt.Fprintf(out, "Usage:\n  ponto summary [flags]\n\n")
		fmt.Fprintf(out, "Summary:\n%s\n", sumFlags.FlagUsages())
		fmt.Fprintf(out, "Input:\n%s", inputFlags.FlagUsages())
		fmt.Fprintf(out, "  -h, --help   Show this help\n")
		return nil
	})

	v.SetEnvPrefix("PONTO")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	v.BindPFlags(cmd.Flags())

	return cmd
}

func run(cmd *cobra.Command, v *viper.Viper) error {
	genImage := v.GetBool("gen-image")
	imageFormat := v.GetString("image-format")
	if genImage && imageFormat != "svg" && imageFormat != "png" {
		return fmt.Errorf("invalid --image-format %q: must be \"svg\" or \"png\"", imageFormat)
	}

	log.Println("Starting Ponto...")

	r, err := buildPonto(cmd, v)
	if err != nil {
		return err
	}
	r.ImageFormat = imageFormat

	// The TUI drives its own asset generation so it can show a spinner.
	if v.GetBool("tui") {
		return runTUI(&r)
	}

	if err := r.generateAssets(); err != nil {
		return err
	}
	log.Println("Done generating assets.")

	fe, err := fs.Sub(frontend, "ui/dist")
	if err != nil {
		return err
	}

	// --gen-image: render the static HTML in a headless browser over file://
	// and drive its export button, then clean up the temp file.
	if genImage {
		tmp, err := os.CreateTemp("", "ponto-*.html")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		tmp.Close()
		defer os.Remove(tmpName)

		if err := r.generateStaticHTML(fe, tmpName); err != nil {
			return err
		}
		screenshot(fileURL(tmpName), r.ImageFormat, r.Output)
		return nil
	}

	if v.GetBool("standalone") {
		log.Println("warning: --standalone is deprecated; static HTML output is now the default")
	}

	htmlName := fmt.Sprintf("%s.html", r.Output)
	if err := r.generateStaticHTML(fe, htmlName); err != nil {
		return err
	}
	log.Printf("Wrote %s", htmlName)

	if v.GetBool("open") {
		return openInBrowser(htmlName)
	}
	return nil
}

func (r *ponto) generateAssets() error {
	// Get Plan
	err := r.getPlan()
	if err != nil {
		return errors.New(fmt.Sprintf("Unable to parse Plan: %s", err))
	}

	// Generate RSO, Map, Graph
	err = r.GenerateResourceOverview()
	if err != nil {
		return err
	}

	err = r.GenerateMap()
	if err != nil {
		return err
	}

	err = r.GenerateGraph()
	if err != nil {
		return err
	}

	return nil
}

func (r *ponto) getPlan() error {
	tmpDir, err := os.MkdirTemp("", "ponto")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tf, err := tfexec.NewTerraform(r.WorkingDir, r.TfPath)
	if err != nil {
		return err
	}

	// With --verbose, stream terraform's own init/plan output so a long init
	// doesn't look like a freeze.
	if r.Verbose {
		tf.SetStdout(os.Stderr)
		tf.SetStderr(os.Stderr)
	}

	// If user provided path to plan file
	if r.PlanPath != "" {
		log.Println("Using provided plan...")
		r.Plan, err = tf.ShowPlanFile(context.Background(), r.PlanPath)
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to read Plan (%s): %s", r.PlanPath, err))
		}
		return nil
	}

	// If user provided path to plan JSON file
	if r.PlanJSONPath != "" {
		log.Println("Using provided JSON plan...")

		planJsonFile, err := os.Open(r.PlanJSONPath)
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to read Plan (%s): %s", r.PlanJSONPath, err))
		}
		defer planJsonFile.Close()

		planJson, err := io.ReadAll(planJsonFile)
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to read Plan (%s): %s", r.PlanJSONPath, err))
		}

		if err := json.Unmarshal(planJson, &r.Plan); err != nil {
			return errors.New(fmt.Sprintf("Unable to read Plan (%s): %s", r.PlanJSONPath, err))
		}

		return nil
	}

	// If user specified TFC workspace
	if r.TFCWorkspaceName != "" {
		tfcToken := os.Getenv("TFC_TOKEN")

		if tfcToken == "" {
			return errors.New("TFC_TOKEN environment variable not set")
		}

		if r.TFCOrgName == "" {
			return errors.New("Must specify Terraform Cloud organization to retrieve plan from Terraform Cloud")
		}

		config := &tfe.Config{
			Token: tfcToken,
		}

		client, err := tfe.NewClient(config)
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to connect to Terraform Cloud. %s", err))
		}

		// Get TFC Workspace
		ws, err := client.Workspaces.Read(context.Background(), r.TFCOrgName, r.TFCWorkspaceName)
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to list workspace %s in %s organization. %s", r.TFCWorkspaceName, r.TFCOrgName, err))
		}

		// Retrieve all runs from specified TFC workspace
		runs, err := client.Runs.List(context.Background(), ws.ID, tfe.RunListOptions{})
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to retrieve plan from %s in %s organization. %s", r.TFCWorkspaceName, r.TFCOrgName, err))
		}

		run := runs.Items[0]

		// Get most recent plan item
		planID := runs.Items[0].Plan.ID

		// Run hasn't been applied or discarded, therefore is still "actionable" by user
		runIsActionable := run.StatusTimestamps.AppliedAt.IsZero() && run.StatusTimestamps.DiscardedAt.IsZero()

		if runIsActionable && r.TFCNewRun {
			return errors.New(fmt.Sprintf("Did not create new run. %s in %s in %s is still active", run.ID, r.TFCWorkspaceName, r.TFCOrgName))
		}

		// If latest run is not actionable, ponto will create new run
		if r.TFCNewRun {
			// Create new run in specified TFC workspace
			newRun, err := client.Runs.Create(context.Background(), tfe.RunCreateOptions{
				Refresh:   &TRUE,
				Workspace: ws,
			})
			if err != nil {
				return errors.New(fmt.Sprintf("Unable to generate new run from %s in %s organization. %s", r.TFCWorkspaceName, r.TFCOrgName, err))
			}

			run = newRun

			log.Printf("Starting new Terraform Cloud run in %s workspace...", r.TFCWorkspaceName)

			// Wait maximum of 5 mins
			for i := 0; i < 30; i++ {
				run, err := client.Runs.Read(context.Background(), newRun.ID)
				if err != nil {
					return errors.New(fmt.Sprintf("Unable to retrieve run from %s in %s organization. %s", r.TFCWorkspaceName, r.TFCOrgName, err))
				}

				if run.Plan != nil {
					planID = run.Plan.ID
					// Add 20 second timeout so plan JSON becomes available
					time.Sleep(20 * time.Second)
					log.Printf("Run %s to completed!", newRun.ID)
					break
				}

				time.Sleep(10 * time.Second)
				log.Printf("Waiting for run %s to complete (%ds)...", newRun.ID, 10*(i+1))
			}

			if planID == "" {
				return errors.New(fmt.Sprintf("Timeout waiting for plan to complete in %s in %s organization. %s", r.TFCWorkspaceName, r.TFCOrgName, err))
			}
		}

		// Get most recent plan file
		planBytes, err := client.Plans.JSONOutput(context.Background(), planID)
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to retrieve plan from %s in %s organization. %s", r.TFCWorkspaceName, r.TFCOrgName, err))
		}
		// If empty plan file
		if string(planBytes) == "" {
			return errors.New(fmt.Sprintf("Empty plan. Check run %s in %s in %s is not pending", run.ID, r.TFCWorkspaceName, r.TFCOrgName))
		}

		if err := json.Unmarshal(planBytes, &r.Plan); err != nil {
			return errors.New(fmt.Sprintf("Unable to parse plan (ID: %s) from %s in %s organization.: %s", planID, r.TFCWorkspaceName, r.TFCOrgName, err))
		}

		return nil
	}

	log.Println("Initializing Terraform...")

	// Create TF Init options
	var tfInitOptions []tfexec.InitOption
	tfInitOptions = append(tfInitOptions, tfexec.Upgrade(true))

	// Add *.tfbackend files
	for _, tfBackendConfig := range r.TfBackendConfigs {
		if tfBackendConfig != "" {
			tfInitOptions = append(tfInitOptions, tfexec.BackendConfig(tfBackendConfig))
		}
	}

	// tfInitOptions = append(tfInitOptions, tfexec.LockTimeout("60s"))

	err = tf.Init(context.Background(), tfInitOptions...)
	if err != nil {
		return errors.New(fmt.Sprintf("Unable to initialize Terraform Plan: %s", err))
	}

	if r.WorkspaceName != "" {
		log.Printf("Running in %s workspace...", r.WorkspaceName)
		err = tf.WorkspaceSelect(context.Background(), r.WorkspaceName)
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to select workspace (%s): %s", r.WorkspaceName, err))
		}
	}

	log.Println("Generating plan...")
	planPath := fmt.Sprintf("%s/%s-%v", tmpDir, "pontoplan", time.Now().Unix())

	// Create TF Plan options
	var tfPlanOptions []tfexec.PlanOption
	tfPlanOptions = append(tfPlanOptions, tfexec.Out(planPath))

	// Add *.tfvars files
	for _, tfVarsFile := range r.TfVarsFiles {
		if tfVarsFile != "" {
			tfPlanOptions = append(tfPlanOptions, tfexec.VarFile(tfVarsFile))
		}
	}

	// Add Terraform variables
	for _, tfVar := range r.TfVars {
		if tfVar != "" {
			tfPlanOptions = append(tfPlanOptions, tfexec.Var(tfVar))
		}
	}

	_, err = tf.Plan(context.Background(), tfPlanOptions...)
	if err != nil {
		return errors.New(fmt.Sprintf("Unable to run Plan: %s", err))
	}

	r.Plan, err = tf.ShowPlanFile(context.Background(), planPath)
	if err != nil {
		return errors.New(fmt.Sprintf("Unable to read Plan: %s", err))
	}

	return nil
}

func showJSON(g interface{}) {
	j, err := json.Marshal(g)
	if err != nil {
		log.Printf("Error producing JSON: %s\n", err)
		os.Exit(2)
	}
	log.Printf("%+v", string(j))
}

func showModuleJSON(module *tfconfig.Module) {
	j, err := json.MarshalIndent(module, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error producing JSON: %s\n", err)
		os.Exit(2)
	}
	os.Stdout.Write(j)
	os.Stdout.Write([]byte{'\n'})
}

func saveJSONToFile(prefix string, fileType string, path string, j interface{}) string {
	b, err := json.Marshal(j)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error producing JSON: %s\n", err)
		os.Exit(2)
	}

	newpath := filepath.Join(".", fmt.Sprintf("%s/%s", path, prefix))
	err = os.MkdirAll(newpath, os.ModePerm)
	if err != nil {
		log.Fatal(err)
	}

	f, err := os.Create(fmt.Sprintf("%s/%s-%s.json", newpath, prefix, fileType))
	if err != nil {
		log.Fatal(err)
	}

	defer f.Close()

	_, err = f.WriteString(string(b))
	if err != nil {
		log.Fatal(err)
	}

	return fmt.Sprintf("%s/%s-%s.json", newpath, prefix, fileType)
}
