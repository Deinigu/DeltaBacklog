## What and why

<!-- What does this change, and why? Link the issue if there is one (Fixes #123). -->

## How I tested it

<!-- Commands run, and for UI changes a screenshot at 390 px and 1280 px (no private data in it). -->

## Checklist

- [ ] `go vet ./... && go test ./...` and `node --test web/test/*.test.mjs` pass
- [ ] A bug fix comes with a test that fails without it
- [ ] The matching section of `docs/user-manual.md` / `docs/developer-manual.md` is updated
- [ ] A line is added under *Unreleased* in `CHANGELOG.md`
- [ ] No frontend build step, no heavy new dependency, no CGO (it must still run on a Raspberry Pi 3B+)
- [ ] No ratings or "number in the series" (removed by design)
- [ ] No secrets or personal data (keys, addresses, real library screenshots)
