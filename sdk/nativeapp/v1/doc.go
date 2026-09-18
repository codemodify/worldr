// Package nativeapp is the version-1 SDK for out-of-process Worldr-native
// applications.
//
// An application implements Application and any optional lifecycle interfaces,
// then calls Serve with stdin and stdout. Worldr owns the process, sends input
// and lifecycle requests, and turns the application's retained resource updates
// into workspace surfaces. Standard output is reserved for the framed protocol;
// applications should write diagnostics to standard error.
//
// Version 1 deliberately exposes data rather than renderer handles. Texture and
// mesh IDs belong to one process connection, surface IDs belong to one running
// application, and stable surface keys are the only identities suitable for a
// saved workspace layout. Calls are serialized. Snapshot data only needs to
// remain unchanged until Snapshot returns because Serve encodes an owned copy
// before reading the next request.
package nativeapp
