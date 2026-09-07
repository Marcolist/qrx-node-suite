package main

// Blank-importing an adapter package is what "installed" means for this
// single-binary Agent -- see agent/adapters/README.md. Remove an import
// here (and rebuild) to ship a smaller binary without that adapter.
import (
	_ "qrx-node-suite/agent/adapters/future"
	_ "qrx-node-suite/agent/adapters/legacy006"
	_ "qrx-node-suite/agent/adapters/mock"
	_ "qrx-node-suite/agent/adapters/qrx007"
)
