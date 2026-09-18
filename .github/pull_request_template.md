## What

<!-- One line: e.g. "Add Foo Editor (Windows, Linux)" or "Fix the Bar entry". Link the issue: "Closes #12". -->

## Dictionary changes

<!-- Delete this section if the pull request does not touch dictionary/*.toml. -->

- [ ] One entry per program, in the right file (`windows.toml`, `linux.toml`, `darwin.toml`) and section
- [ ] Tested locally: with the entry in `apps.user.toml`, `husk -v --show all` shows the expected status
      (`associated` while installed, `orphan` after uninstalling, `shared` for `[[shared]]`)
- [ ] Paths use variables (`%APPDATA%`, `~`), no user names
- [ ] `go test ./dictionary` passes

## Code changes

<!-- Delete this section if the pull request only touches the dictionary. -->

- [ ] `gofmt -l .`, `go vet ./...` and `go test ./...` are clean
- [ ] README / CONTRIBUTING updated if the behaviour or the format changed
