package bundle

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/teatest"
	"github.com/defenseunicorns/uds-cli/src/config"
	"github.com/defenseunicorns/uds-cli/src/pkg/bundle/tui/deploy"
	"github.com/defenseunicorns/uds-cli/src/pkg/utils"
	"github.com/defenseunicorns/uds-cli/src/types"
	"github.com/stretchr/testify/require"
)

func TestDeploy(t *testing.T) {
	config.CommonOptions.Confirm = true
	config.CommonOptions.CachePath = "~/.uds-cache"
	err := utils.ConfigureLogs("deploy")
	require.NoError(t, err)
	bndlClient := NewOrDie(&types.BundleConfig{
		DeployOpts: types.BundleDeployOptions{
			Source: "ghcr.io/unclegedd/ghcr-test:0.0.1",
		},
	})
	m := deploy.InitModel(bndlClient)
	tm := teatest.NewTestModel(t, &m, teatest.WithInitialTermSize(50, 100))
	deploy.Program = tm.GetProgram()

	t.Cleanup(func() {
		if err := tm.Quit(); err != nil {
			t.Fatal(err)
		}
	})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Validating bundle"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(time.Millisecond*10))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("UDS Bundle: ghcr-test")) &&
			bytes.Contains(out, []byte("Deploying bundle package (1 / 2)")) &&
			bytes.Contains(out, []byte("<l> Toggle logs"))
	}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(time.Millisecond*10))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Verifying podinfo package"))
	}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(time.Millisecond*10))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Downloading podinfo package"))
	}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(time.Millisecond*10))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Deploying podinfo package (1 / 1 components)"))
	}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(time.Millisecond*10))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		fmt.Println(string(out))
		return bytes.Contains(out, []byte("Verifying nginx package"))
	}, teatest.WithDuration(30*time.Second), teatest.WithCheckInterval(time.Millisecond*10))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Downloading nginx package"))
	}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(time.Millisecond*10))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Deploying nginx package (1 / 1 components)"))
	}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(time.Millisecond*10))

}
