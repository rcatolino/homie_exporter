package main

// Represent the actual measurement node metadata
type Property struct {
	name    string
	unit    string
	ignored bool
}

// Represent the device metadata
// Actually for homie this should be a device Node, but I'm not parsing root device properties,
// so let's simplify this a bit
type Device struct {
	path       string
	name       string
	properties map[string]Property
}
