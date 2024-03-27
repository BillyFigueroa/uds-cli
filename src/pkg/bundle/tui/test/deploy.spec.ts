import {test, expect, Shell} from "@microsoft/tui-test";


test.use({ shell: Shell.Bash, rows: 150, columns: 150 });

test("take a screenshot", async ({ terminal }) => {
  terminal.submit("./../../../../../build/uds-mac-apple deploy ghcr.io/unclegedd/ghcr-test:0.0.1 --confirm")
  const buf = terminal.getBuffer()
  console.log("hello world")
  // await expect(terminal).toMatchSnapshot();
});
