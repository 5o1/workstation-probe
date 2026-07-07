module github.com/assaneko/workstation-probe/webview/hugo-test-basic

go 1.21

require github.com/assaneko/workstation-probe/webview/hugo v0.0.0

// Resolve the module import declared in hugo.yaml to the local
// working tree. The relative path keeps this site portable across
// clones (no absolute home path in version control).
replace github.com/assaneko/workstation-probe/webview/hugo => ../..
