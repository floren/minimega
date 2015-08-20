// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io/ioutil"
	"minicli"
	log "minilog"
	"os"
	"path/filepath"
	"ranges"
	"strconv"
	"strings"
)

var vmCLIHandlers = []minicli.Handler{
	{ // vm info
		HelpShort: "print information about VMs",
		HelpLong: `
Print information about VMs in tabular form. The .filter and .columns commands
can be used to subselect a set of rows and/or columns. See the help pages for
.filter and .columns, respectively, for their usage. Columns returned by VM
info include:

- host      : the host that the VM is running on
- id        : the VM ID, as an integer
- name      : the VM name, if it exists
- state     : one of (building, running, paused, quit, error)
- type      : one of (kvm)
- vcpus     : the number of allocated CPUs
- memory    : allocated memory, in megabytes
- vlan      : vlan, as an integer
- bridge    : bridge name
- tap       : tap name
- mac       : mac address
- ip        : IPv4 address
- ip6       : IPv6 address
- bandwidth : stats regarding bandwidth usage
- tags      : any additional information attached to the VM

Additional fields are available for KVM-based VMs:

- append    : kernel command line string
- cdrom     : cdrom image
- disk      : disk image
- kernel    : kernel image
- initrd    : initrd image
- migrate   : qemu migration image
- uuid      : QEMU system uuid
- cc_active : whether cc is active

Additional fields are available for Android-based VMs:

- number    : telephone number of the VM

Note: all KVM-based fields are available for Android-based VMs.

Examples:

Display a list of all IPs for all VMs:
	.columns ip,ip6 vm info

Display all information about KVM-based VMs with the disk image foo.qc2:
	.filter disk=foo.qc2 vm info kvm

Display information about all VMs:
	vm info`,
		Patterns: []string{
			"vm info",
			"vm info <kvm,>",
			"vm info <android,>",
		},
		Call: wrapSimpleCLI(cliVmInfo),
	},
	{ // vm save
		HelpShort: "save a vm configuration for later use",
		HelpLong: `
Saves the configuration of a running virtual machine or set of virtual machines
so that it/they can be restarted/recovered later, such as after a system crash.

This command does not store the state of the virtual machine itself, only its
launch configuration.`,
		Patterns: []string{
			"vm save <name> <vm id or name or all>...",
		},
		Call: wrapSimpleCLI(cliVmSave),
	},
	{ // vm launch
		HelpShort: "launch virtual machines in a paused state",
		HelpLong: fmt.Sprintf(`
Launch virtual machines in a paused state, using the parameters defined leading
up to the launch command. Any changes to the VM parameters after launching will
have no effect on launched VMs.

When you launch a VM, you supply the type of VM in the launch command.
Currently, the supported VM types are:

- kvm : QEMU-based vms

If you supply a name instead of a number of VMs, one VM with that name will be
launched. You may also supply a range expression to launch VMs with a specific
naming scheme:

	vm launch foo[0-9]

Note: VM names cannot be integers or reserved words (e.g. "%[1]s").

The optional 'noblock' suffix forces minimega to return control of the command
line immediately instead of waiting on potential errors from launching the
VM(s). The user must check logs or error states from vm info.`, Wildcard),
		Patterns: []string{
			"vm launch <kvm,> <name or count> [noblock,]",
			"vm launch <android,> <name or count> [noblock,]",
		},
		Call: wrapSimpleCLI(cliVmLaunch),
	},
	{ // vm kill
		HelpShort: "kill running virtual machines",
		HelpLong: `
Kill one or more running virtual machines. See "vm start" for a full
description of allowable targets.`,
		Patterns: []string{
			"vm kill <target>",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmApply(c, func(target string) []error {
				return vms.kill(target)
			})
		}),
	},
	{ // vm start
		HelpShort: "start paused virtual machines",
		HelpLong: fmt.Sprintf(`
Start one or more paused virtual machines. VMs may be selected by name, ID, range, or
wildcard. For example,

To start vm foo:

		vm start foo

To start vms foo and bar:

		vm start foo,bar

To start vms foo0, foo1, foo2, and foo5:

		vm start foo[0-2,5]

VMs can also be specified by ID, such as:

		vm start 0

Or, a range of IDs:

		vm start [2-4,6]

There is also a wildcard (%[1]s) which allows the user to specify all VMs:

		vm start %[1]s

Note that including the wildcard in a list of VMs results in the wildcard
behavior (although a message will be logged).

Calling "vm start" on a specific list of VMs will cause them to be started if
they are in the building, paused, quit, or error states. When used with the
wildcard, only vms in the building or paused state will be started.`, Wildcard),
		Patterns: []string{
			"vm start <target>",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmApply(c, func(target string) []error {
				return vms.start(target)
			})
		}),
		Suggest: func(val, prefix string) []string {
			if val == "target" {
				return cliVMSuggest(prefix, ^VM_RUNNING)
			} else {
				return nil
			}
		},
	},
	{ // vm stop
		HelpShort: "stop/pause virtual machines",
		HelpLong: `
Stop one or more running virtual machines. See "vm start" for a full
description of allowable targets.

Calling stop will put VMs in a paused state. Use "vm start" to restart them.`,
		Patterns: []string{
			"vm stop <target>",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmApply(c, func(target string) []error {
				return vms.stop(target)
			})
		}),
		Suggest: func(val, prefix string) []string {
			if val == "target" {
				return cliVMSuggest(prefix, VM_RUNNING)
			} else {
				return nil
			}
		},
	},
	{ // vm flush
		HelpShort: "discard information about quit or failed VMs",
		HelpLong: `
Discard information about VMs that have either quit or encountered an error.
This will remove any VMs with a state of "quit" or "error" from vm info. Names
of VMs that have been flushed may be reused.`,
		Patterns: []string{
			"vm flush",
		},
		Call: wrapSimpleCLI(cliVmFlush),
	},
	{ // vm hotplug
		HelpShort: "add and remove USB drives",
		HelpLong: `
Add and remove USB drives to a launched VM.

To view currently attached media, call vm hotplug with the 'show' argument and
a VM ID or name. To add a device, use the 'add' argument followed by the VM ID
or name, and the name of the file to add. For example, to add foo.img to VM 5:

	vm hotplug add 5 foo.img

The add command will assign a disk ID, shown in vm hotplug show. To remove
media, use the 'remove' argument with the VM ID and the disk ID. For example,
to remove the drive added above, named 0:

	vm hotplug remove 5 0

To remove all hotplug devices, use ID "all" for the disk ID.`,
		Patterns: []string{
			"vm hotplug <show,> <vm id or name>",
			"vm hotplug <add,> <vm id or name> <filename>",
			"vm hotplug <remove,> <vm id or name> <disk id or all>",
		},
		Call: wrapSimpleCLI(cliVmHotplug),
		Suggest: func(val, prefix string) []string {
			if val == "vm" {
				return cliVMSuggest(prefix, VM_ANY_STATE)
			} else {
				return nil
			}
		},
	},
	{ // vm net
		HelpShort: "disconnect or move network connections",
		HelpLong: `
Disconnect or move existing network connections on a running VM.

Network connections are indicated by their position in vm net (same order in vm
info) and are zero indexed. For example, to disconnect the first network
connection from a VM named vm-0 with 4 network connections:

	vm net disconnect vm-0 0

To disconnect the second connection:

	vm net disconnect vm-0 1

To move a connection, specify the new VLAN tag and bridge:

	vm net <vm name or id> 0 bridgeX 100`,
		Patterns: []string{
			"vm net <connect,> <vm id or name> <tap position> <bridge> <vlan>",
			"vm net <disconnect,> <vm id or name> <tap position>",
		},
		Call: wrapSimpleCLI(cliVmNetMod),
		Suggest: func(val, prefix string) []string {
			if val == "vm" {
				return cliVMSuggest(prefix, VM_ANY_STATE)
			} else {
				return nil
			}
		},
	},
	{ // vm qmp
		HelpShort: "issue a JSON-encoded QMP command",
		HelpLong: `
Issue a JSON-encoded QMP command. This is a convenience function for accessing
the QMP socket of a VM via minimega. vm qmp takes two arguments, a VM ID or
name, and a JSON string, and returns the JSON encoded response. For example:

	vm qmp 0 '{ "execute": "query-status" }'
	{"return":{"running":false,"singlestep":false,"status":"prelaunch"}}`,
		Patterns: []string{
			"vm qmp <vm id or name> <qmp command>",
		},
		Call: wrapSimpleCLI(cliVmQmp),
		Suggest: func(val, prefix string) []string {
			if val == "vm" {
				return cliVMSuggest(prefix, VM_ANY_STATE)
			} else {
				return nil
			}
		},
	},
	{ // vm screenshot
		HelpShort: "take a screenshot of a running vm",
		HelpLong: `
Take a screenshot of the framebuffer of a running VM. The screenshot is saved
in PNG format as "screenshot.png" in the VM's runtime directory (by default
/tmp/minimega/<vm id>/screenshot.png).

An optional argument sets the maximum dimensions in pixels, while keeping the
aspect ratio. For example, to set either maximum dimension of the output image
to 100 pixels:

	vm screenshot foo 100

The screenshot can be saved elsewhere like this:

        vm screenshot foo file /tmp/foo.png

You can also specify the maximum dimension:

        vm screenshot foo file /tmp/foo.png 100`,
		Patterns: []string{
			"vm screenshot <vm id or name> [maximum dimension]",
			"vm screenshot <vm id or name> file <filename> [maximum dimension]",
		},
		Call: wrapSimpleCLI(cliVmScreenshot),
		Suggest: func(val, prefix string) []string {
			if val == "vm" {
				return cliVMSuggest(prefix, VM_ANY_STATE)
			} else {
				return nil
			}
		},
	},
	{ // vm migrate
		HelpShort: "write VM state to disk",
		HelpLong: `
Migrate runtime state of a VM to disk, which can later be booted with vm config
migrate.

Migration files are written to the files directory as specified with -filepath.
On success, a call to migrate a VM will return immediately. You can check the
status of in-flight migrations by invoking vm migrate with no arguments.`,
		Patterns: []string{
			"vm migrate",
			"vm migrate <vm id or name> <filename>",
		},
		Call: wrapSimpleCLI(cliVmMigrate),
		Suggest: func(val, prefix string) []string {
			if val == "vm" {
				return cliVMSuggest(prefix, VM_ANY_STATE)
			} else {
				return nil
			}
		},
	},
	{ // vm tag
		HelpShort: "display or set a tag for the specified VM",
		HelpLong: `
Display or set a tag for one or more virtual machines. See "vm start" for a
full description of allowable targets.

Tags are key-value pairs. A VM can have any number of tags associated with it.
They can be used to attach additional information to a virtual machine, for
example specifying a VM "group", or the correct rendering color for some
external visualization tool.

To set a tag "foo" to "bar" for VM 2:

        vm tag 2 foo bar

To read a tag:

        vm tag <target> <key or all>`,
		Patterns: []string{
			"vm tag <target> [key or all]",  // get
			"vm tag <target> <key> <value>", // set
		},
		Call: wrapSimpleCLI(cliVmTag),
		Suggest: func(val, prefix string) []string {
			if val == "target" {
				return cliVMSuggest(prefix, VM_ANY_STATE)
			} else {
				return nil
			}
		},
	},
	{ // vm cdrom
		HelpShort: "eject or change an active VM's cdrom",
		HelpLong: `
Eject or change an active VM's cdrom image.

Eject VM 0's cdrom:

        vm cdrom eject 0

Eject all VM cdroms:

        vm cdrom eject all

Change a VM to use a new ISO:

        vm cdrom change 0 /tmp/debian.iso

"vm cdrom change" implies that the current ISO will be ejected.`,
		Patterns: []string{
			"vm cdrom <eject,> <vm id or name>",
			"vm cdrom <change,> <vm id or name> <path>",
		},
		Call: wrapSimpleCLI(cliVmCdrom),
		Suggest: func(val, prefix string) []string {
			if val == "vm" {
				return cliVMSuggest(prefix, VM_ANY_STATE)
			} else {
				return nil
			}
		},
	},
	{ // vm config
		HelpShort: "display, save, or restore the current VM configuration",
		HelpLong: `
Display, save, or restore the current VM configuration. Note that saving and
restoring configuration applies to all VM configurations including KVM-based VM
configurations.

To display the current configuration, call vm config with no arguments.

List the current saved configurations with 'vm config restore'.

To save a configuration:

	vm config save <config name>

To restore a configuration:

	vm config restore <config name>

To clone the configuration of an existing VM:

	vm config clone <vm name or id>

Calling clear vm config will clear all VM configuration options, but will not
remove saved configurations.`,
		Patterns: []string{
			"vm config",
			"vm config <save,> <name>",
			"vm config <restore,> [name]",
			"vm config <clone,> <vm id or name>",
		},
		Call: wrapSimpleCLI(cliVmConfig),
	},
	{ // vm config memory
		HelpShort: "set the amount of physical memory for a VM",
		HelpLong: `
Set the amount of physical memory to allocate in megabytes.`,
		Patterns: []string{
			"vm config memory [memory in megabytes]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "memory")
		}),
	},
	{ // vm config vcpus
		HelpShort: "set the number of virtual CPUs for a VM",
		HelpLong: `
Set the number of virtual CPUs to allocate for a VM.`,
		Patterns: []string{
			"vm config vcpus [number of CPUs]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "vcpus")
		}),
	},
	{ // vm config net
		HelpShort: "specific the networks a VM is a member of",
		HelpLong: `
Specify the network(s) that the VM is a member of by VLAN. A corresponding VLAN
will be created for each network. Optionally, you may specify the bridge the
interface will be connected on. If the bridge name is omitted, minimega will
use the default 'mega_bridge'. You can also optionally specify the mac address
of the interface to connect to that network. If not specifed, the mac address
will be randomly generated. Additionally, you can optionally specify a driver
for qemu to use. By default, e1000 is used.

Examples:

To connect a VM to VLANs 1 and 5:

	vm config net 1 5

To connect a VM to VLANs 100, 101, and 102 with specific mac addresses:

	vm config net 100,00:00:00:00:00:00 101,00:00:00:00:01:00 102,00:00:00:00:02:00

To connect a VM to VLAN 1 on bridge0 and VLAN 2 on bridge1:

	vm config net bridge0,1 bridge1,2

To connect a VM to VLAN 100 on bridge0 with a specific mac:

	vm config net bridge0,100,00:11:22:33:44:55

To specify a specific driver, such as i82559c:

	vm config net 100,i82559c

Calling vm net with no parameters will list the current networks for this VM.`,
		Patterns: []string{
			"vm config net [netspec]...",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "net")
		}),
	},
	{ // vm config append
		HelpShort: "set an append string to pass to a kernel set with vm kernel",
		HelpLong: `
Add an append string to a kernel set with vm kernel. Setting vm append without
using vm kernel will result in an error.

For example, to set a static IP for a linux VM:

	vm kvm config append ip=10.0.0.5 gateway=10.0.0.1 netmask=255.255.255.0 dns=10.10.10.10

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config append [arg]...",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "append")
		}),
	},
	{ // vm config qemu
		HelpShort: "set the QEMU process to invoke. Relative paths are ok.",
		HelpLong: `
Set the QEMU process to invoke. Relative paths are ok.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config qemu [path to qemu]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "qemu")
		}),
	},
	{ // vm config qemu-override
		HelpShort: "override parts of the QEMU launch string",
		HelpLong: `
Override parts of the QEMU launch string by supplying a string to match, and a
replacement string.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config qemu-override",
			"vm config qemu-override add <match> <replacement>",
			"vm config qemu-override delete <id or all>",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "qemu-override")
		}),
	},
	{ // vm config qemu-append
		HelpShort: "add additional arguments to the QEMU command",
		HelpLong: `
Add additional arguments to be passed to the QEMU instance. For example:

	vm kvm config qemu-append -serial tcp:localhost:4001

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config qemu-append [argument]...",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "qemu-append")
		}),
	},
	{ // vm config migrate
		HelpShort: "set migration image for a saved VM",
		HelpLong: `
Assign a migration image, generated by a previously saved VM to boot with.
Migration images should be booted with a kernel/initrd, disk, or cdrom. Use 'vm
migrate' to generate migration images from running VMs.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config migrate [path to migration image]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "migrate")
		}),
	},
	{ // vm config disk
		HelpShort: "set disk images to attach to a VM",
		HelpLong: `
Attach one or more disks to a vm. Any disk image supported by QEMU is a valid
parameter. Disk images launched in snapshot mode may safely be used for
multiple VMs.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config disk [path to disk image]...",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "disk")
		}),
	},
	{ // vm config cdrom
		HelpShort: "set a cdrom image to attach to a VM",
		HelpLong: `
Attach a cdrom to a VM. When using a cdrom, it will automatically be set to be
the boot device.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config cdrom [path to cdrom image]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "cdrom")
		}),
	},
	{ // vm config kernel
		HelpShort: "set a kernel image to attach to a VM",
		HelpLong: `
Attach a kernel image to a VM. If set, QEMU will boot from this image instead
of any disk image.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config kernel [path to kernel]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "kernel")
		}),
	},
	{ // vm config initrd
		HelpShort: "set a initrd image to attach to a VM",
		HelpLong: `
Attach an initrd image to a VM. Passed along with the kernel image at boot
time.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config initrd [path to initrd]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "initrd")
		}),
	},
	{ // vm config uuid
		HelpShort: "set the UUID for a VM",
		HelpLong: `
Set the UUID for a virtual machine. If not set, minimega will create a random
one when the VM is launched.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config uuid [uuid]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "uuid")
		}),
	},
	{ // vm config serial
		HelpShort: "specify the serial ports a VM will use",
		HelpLong: `
Specify the serial ports that will be created for the VM to use.
Serial ports specified will be mapped to the VM's /dev/ttySX device, where X
refers to the connected unix socket on the host at
$minimega_runtime/<vm_id>/serialX.

Examples:

To display current serial ports:
  vm config serial

To create three serial ports:
  vm config serial 3

Note: Whereas modern versions of Windows support up to 256 COM ports, Linux
typically only supports up to four serial devices. To use more, make sure to
pass "8250.n_uarts = 4" to the guest Linux kernel at boot. Replace 4 with
another number.`,
		Patterns: []string{
			"vm config serial [number of serial ports]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "serial")
		}),
	},
	{ // vm config virtio-serial
		HelpShort: "specify the virtio-serial ports a VM will use",
		HelpLong: `
Specify the virtio-serial ports that will be created for the VM to use.
Virtio-serial ports specified will be mapped to the VM's
/dev/virtio-port/<portname> device, where <portname> refers to the connected
unix socket on the host at $minimega_runtime/<vm_id>/virtio-serialX.

Examples:

To display current virtio-serial ports:
  vm config virtio-serial

To create three virtio-serial ports:
  vm config virtio-serial 3`,
		Patterns: []string{
			"vm config virtio-serial [number of virtio-serial ports]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "virtio-serial")
		}),
	},
	{ // vm config snapshot
		HelpShort: "enable or disable snapshot mode when using disk images",
		HelpLong: `
Enable or disable snapshot mode when using disk images. When enabled, disks
images will be loaded in memory when run and changes will not be saved. This
allows a single disk image to be used for many VMs.

Note: this configuration only applies to KVM-based VMs.`,
		Patterns: []string{
			"vm config snapshot [true,false]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "snapshot")
		}),
	},
	{ // vm config telephony
		HelpShort: "configure the telephone network of a mobile VM",
		HelpLong: `
Set the telephone network number prefix for newly launched VMs. For example:

	vm config telephony 223344

Would cause newly launched VMs to be assigned numbers 2233440000, 2233440001,
and so on. Number blocks should not overlap, the following produces and error:

	vm config telephony 223344
	vm config telephony 22334

Note: this configuration only applies to Android-based VMs.`,
		Patterns: []string{
			"vm config telephony [prefix]",
		},
		Call: wrapSimpleCLI(func(c *minicli.Command) *minicli.Response {
			return cliVmConfigField(c, "telephony")
		}),
	},
	{ // clear vm config
		HelpShort: "reset vm config to the default value",
		HelpLong: `
Resets the configuration for a provided field (or the whole configuration) back
to the default value.`,
		// HACK: These patterns could be reduced to a single pattern with all
		// the different config fields as one multiple choice, however, to make
		// it easier to read, we split them into separate patterns. We could
		// use string literals for the field names but then we'd have to
		// process the Original string within the Command struct to figure out
		// what field we're supposed to clear. Instead, we can leverage the
		// magic of single-choice fields to set the field name in BoolArgs.
		Patterns: []string{
			"clear vm config",
			// VMConfig
			"clear vm config <memory,>",
			"clear vm config <net,>",
			"clear vm config <vcpus,>",
			// KVMConfig
			"clear vm config <append,>",
			"clear vm config <cdrom,>",
			"clear vm config <migrate,>",
			"clear vm config <disk,>",
			"clear vm config <initrd,>",
			"clear vm config <kernel,>",
			"clear vm config <qemu,>",
			"clear vm config <qemu-append,>",
			"clear vm config <qemu-override,>",
			"clear vm config <snapshot,>",
			"clear vm config <uuid,>",
			"clear vm config <serial,>",
			"clear vm config <virtio-serial,>",
			// AndroidConfig
			"clear vm config <telephony,>",
		},
		Call: wrapSimpleCLI(cliClearVmConfig),
	},
	{ // clear vm tag
		HelpShort: "remove tags from a VM",
		HelpLong: `
Clears one, many, or all tags from a virtual machine.

Clear the tag "foo" from VM 0:

        clear vm tag 0 foo

Clear the tag "foo" from all VMs:

        clear vm tag all foo

Clear all tags from VM 0:

        clear vm tag 0

Clear all tags from all VMs:

        clear vm tag all`,
		Patterns: []string{
			"clear vm tag",
			"clear vm tag <target> [tag]",
		},
		Call: wrapSimpleCLI(cliClearVmTag),
	},
}

func init() {
	registerHandlers("vm", vmCLIHandlers)

	// Register these so we can serialize the VMs
	gob.Register(VMs{})
	gob.Register(&KvmVM{})
}

func cliVmInfo(c *minicli.Command) *minicli.Response {
	var err error
	resp := &minicli.Response{Host: hostname}

	vmType := ""
	if c.BoolArgs["kvm"] {
		vmType = "kvm"
	} else if c.BoolArgs["android"] {
		vmType = "android"
	}

	for _, vm := range vms {
		// Populate the latest bandwidth stats for all VMs
		vm.UpdateBW()

		// Populate CC Active flag for KVM vms
		if vm, ok := vm.(*KvmVM); ok {
			vm.UpdateCCActive()
		}
	}

	resp.Header, resp.Tabular, err = vms.info(vmType)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	resp.Data = vms

	return resp
}

func cliVmCdrom(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	vmstring := c.StringArgs["vm"]
	doVms := make([]*KvmVM, 0)
	if vmstring == Wildcard {
		for _, vm := range vms {
			switch vm := vm.(type) {
			case *KvmVM:
				doVms = append(doVms, vm)
			default:
				// TODO: Do anything?
			}
		}
	} else {
		vm := vms.findVm(vmstring)
		if vm == nil {
			resp.Error = vmNotFound(vmstring).Error()
			return resp
		}
		if vm, ok := vm.(*KvmVM); ok {
			doVms = append(doVms, vm)
		} else {
			resp.Error = "cdrom commands are only supported for kvm vms"
			return resp
		}
	}

	if c.BoolArgs["eject"] {
		for _, v := range doVms {
			err := v.q.BlockdevEject("ide0-cd1")
			v.CdromPath = ""
			if err != nil {
				resp.Error = err.Error()
				return resp
			}
		}
	} else if c.BoolArgs["change"] {
		for _, v := range doVms {
			// First eject it, then change it
			err := v.q.BlockdevEject("ide0-cd1")
			v.CdromPath = ""
			if err != nil {
				resp.Error = err.Error()
				return resp
			}

			err = v.q.BlockdevChange("ide0-cd1", c.StringArgs["path"])
			v.CdromPath = c.StringArgs["path"]
			if err != nil {
				resp.Error = err.Error()
				return resp
			}
		}

	}

	return resp
}

func cliVmTag(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	key := c.StringArgs["key"]
	if key == "" {
		// If they didn't specify a key then they probably want all the tags
		// for a given VM
		key = Wildcard
	}

	value, setOp := c.StringArgs["value"]
	if setOp {
		if key == Wildcard {
			// Can't assign a value to wildcard!
			resp.Error = "cannot assign to wildcard"
			return resp
		}
	} else {
		if key == Wildcard {
			resp.Header = []string{"ID", "Tag", "Value"}
		} else {
			resp.Header = []string{"ID", "Value"}
		}

		resp.Tabular = make([][]string, 0)
	}

	target := c.StringArgs["target"]

	errs := expandVmTargets(target, false, func(vm VM, wild bool) (bool, error) {
		if setOp {
			vm.GetTags()[key] = value
		} else if key == Wildcard {
			for k, v := range vm.GetTags() {
				resp.Tabular = append(resp.Tabular, []string{
					strconv.Itoa(vm.GetID()),
					k, v,
				})
			}
		} else {
			// TODO: return false if tag not set?
			resp.Tabular = append(resp.Tabular, []string{
				strconv.Itoa(vm.GetID()),
				vm.GetTags()[key],
			})
		}

		return true, nil
	})

	if len(errs) > 0 {
		resp.Error = errSlice(errs).String()
	}

	return resp
}

func cliClearVmTag(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	key := c.StringArgs["key"]
	if key == "" {
		// If they didn't specify a key then they probably want all the tags
		// for a given VM
		key = Wildcard
	}

	target, ok := c.StringArgs["target"]
	if !ok {
		// No target specified, must want to clear all
		target = Wildcard
	}

	errs := expandVmTargets(target, true, func(vm VM, wild bool) (bool, error) {
		if key == Wildcard {
			vm.ClearTags()
		} else {
			delete(vm.GetTags(), key)
		}

		return true, nil
	})

	if len(errs) > 0 {
		resp.Error = errSlice(errs).String()
	}

	return resp
}

func cliVmConfig(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	if c.BoolArgs["save"] {
		// Save the current config
		savedInfo[c.StringArgs["name"]] = *vmConfig.Copy()
	} else if c.BoolArgs["restore"] {
		if name, ok := c.StringArgs["name"]; ok {
			// Try to restore an existing config
			if s, ok := savedInfo[name]; ok {
				vmConfig = *s.Copy()
			} else {
				resp.Error = fmt.Sprintf("config %v does not exist", name)
			}
		} else if len(savedInfo) == 0 {
			resp.Error = "no vm configs saved"
		} else {
			// List the save configs
			for k := range savedInfo {
				resp.Response += fmt.Sprintln(k)
			}
		}
	} else if c.BoolArgs["clone"] {
		// Clone the config of an existing vm
		vm := vms.findVm(c.StringArgs["vm"])
		if vm == nil {
			resp.Error = vmNotFound(c.StringArgs["vm"]).Error()
		} else {
			vmConfig.BaseConfig = *vm.Config().Copy()
			switch vm := vm.(type) {
			case *KvmVM:
				vmConfig.KVMConfig = *vm.KVMConfig.Copy()
			case *AndroidVM:
				vmConfig.AndroidConfig = *vm.AndroidConfig.Copy()
			}
		}
	} else {
		// Print the config
		resp.Response = vmConfig.String()
	}

	return resp
}

func cliVmConfigField(c *minicli.Command, field string) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	// If there are no args it means that we want to display the current value
	nArgs := len(c.StringArgs) + len(c.ListArgs) + len(c.BoolArgs)

	var ok bool
	var fns VMConfigFns
	var config interface{}

	// Find the right config functions, baseConfigFns has highest priority
	if fns, ok = baseConfigFns[field]; ok {
		config = &vmConfig.BaseConfig
	} else if fns, ok = kvmConfigFns[field]; ok {
		config = &vmConfig.KVMConfig
	} else if fns, ok = androidConfigFns[field]; ok {
		config = &vmConfig.AndroidConfig
	} else {
		log.Fatalln("unknown config field: `%s`", field)
	}

	if nArgs == 0 {
		resp.Response = fns.Print(config)
	} else {
		if err := fns.Update(config, c); err != nil {
			resp.Error = err.Error()
		}
	}

	return resp
}

func cliClearVmConfig(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	var clearAll = len(c.BoolArgs) == 0
	var cleared bool

	for k, fns := range baseConfigFns {
		if clearAll || c.BoolArgs[k] {
			fns.Clear(&vmConfig.BaseConfig)
			cleared = true
		}
	}
	for k, fns := range kvmConfigFns {
		if clearAll || c.BoolArgs[k] {
			fns.Clear(&vmConfig.KVMConfig)
			cleared = true
		}
	}
	for k, fns := range androidConfigFns {
		if clearAll || c.BoolArgs[k] {
			fns.Clear(&vmConfig.AndroidConfig)
			cleared = true
		}
	}

	if !cleared {
		log.Fatalln("no callback defined for clear")
	}

	return resp
}

func cliVmLaunch(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	arg := c.StringArgs["name"]
	names := []string{}

	count, err := strconv.ParseInt(arg, 10, 32)
	if err != nil {
		names, err = ranges.SplitList(arg)
	} else if count <= 0 {
		err = errors.New("invalid number of vms (must be > 0)")
	} else {
		for i := int64(0); i < count; i++ {
			names = append(names, "")
		}
	}

	if len(names) == 0 && err == nil {
		err = errors.New("no VMs to launch")
	}

	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	for _, name := range names {
		if isReserved(name) {
			resp.Error = fmt.Sprintf("`%s` is a reserved word -- cannot use for vm name", name)
			return resp
		}

		if _, err := strconv.Atoi(name); err == nil {
			resp.Error = fmt.Sprintf("`%s` is an integer -- cannot use for vm name", name)
			return resp
		}

		for _, vm := range vms {
			if vm.GetName() == name {
				resp.Error = fmt.Sprintf("`%s` is already the name of a VM", name)
				return resp
			}
		}
	}

	noblock := c.BoolArgs["noblock"]
	delete(c.BoolArgs, "noblock")

	// Parse the VM type, at this point there should only be one key left in
	// BoolArgs and it should be the VM type.
	var vmType VMType
	for k := range c.BoolArgs {
		var err error
		vmType, err = ParseVMType(k)
		if err != nil {
			log.Fatalln("expected VM type, not `%v`", k)
		}
	}

	log.Info("launching %v %v vms", len(names), vmType)

	ack := make(chan int)
	waitForAcks := func(count int) {
		// get acknowledgements from each vm
		for i := 0; i < count; i++ {
			log.Debug("launch ack from VM %v", <-ack)
		}
	}

	for i, name := range names {
		if err := vms.launch(name, vmType, ack); err != nil {
			resp.Error = err.Error()
			go waitForAcks(i)
			return resp
		}
	}

	if noblock {
		go waitForAcks(len(names))
	} else {
		waitForAcks(len(names))
	}

	return resp
}

// cliVmApply is a wrapper function that runs the provided function on the
// ``target'' of the command. This is useful as many VM-related commands take a
// single target (e.g. start, stop).
func cliVmApply(c *minicli.Command, fn func(string) []error) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	errs := fn(c.StringArgs["target"])
	if len(errs) > 0 {
		resp.Error = errSlice(errs).String()
	}

	return resp
}

func cliVmFlush(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	vms.flush()

	return resp
}

func cliVmQmp(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	var err error
	resp.Response, err = vms.qmp(c.StringArgs["vm"], c.StringArgs["qmp"])
	if err != nil {
		resp.Error = err.Error()
	}

	return resp
}

func cliVmScreenshot(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	vm := c.StringArgs["vm"]
	maximum := c.StringArgs["maximum"]
	file := c.StringArgs["filename"]

	var max int
	var err error
	if maximum != "" {
		max, err = strconv.Atoi(maximum)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
	}

	v := vms.findVm(vm)
	if v == nil {
		resp.Error = vmNotFound(vm).Error()
		return resp
	}

	path := filepath.Join(*f_base, fmt.Sprintf("%v", v.GetID()), "screenshot.png")
	if file != "" {
		path = file
	}

	pngData, err := vms.screenshot(vm, path, max)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	// add user data in case this is going across meshage
	err = ioutil.WriteFile(path, pngData, os.FileMode(0644))
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	resp.Data = pngData

	return resp
}

func cliVmMigrate(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	var err error

	if _, ok := c.StringArgs["vm"]; !ok { // report current migrations
		// tabular data is
		// 	vm id, vm name, migrate status, % complete
		for _, vm := range vms {
			vm, ok := vm.(*KvmVM)
			if !ok {
				// TODO: remove?
				continue
			}

			status, complete, err := vm.QueryMigrate()
			if err != nil {
				resp.Error = err.Error()
				return resp
			}
			if status == "" {
				continue
			}
			resp.Tabular = append(resp.Tabular, []string{
				fmt.Sprintf("%v", vm.GetID()),
				vm.GetName(),
				status,
				fmt.Sprintf("%.2f", complete)})
		}
		if len(resp.Tabular) != 0 {
			resp.Header = []string{"vm id", "vm name", "status", "%% complete"}
		}
		return resp
	}

	err = vms.migrate(c.StringArgs["vm"], c.StringArgs["filename"])
	if err != nil {
		resp.Error = err.Error()
	}

	return resp
}

func cliVmSave(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	path := filepath.Join(*f_base, "saved_vms")
	err := os.MkdirAll(path, 0775)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	name := c.StringArgs["name"]
	file, err := os.Create(filepath.Join(path, name))
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	err = vms.save(file, c.ListArgs["vm"])
	if err != nil {
		resp.Error = err.Error()
	}

	return resp
}

func cliVmHotplug(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	vm := vms.findVm(c.StringArgs["vm"])
	if vm == nil {
		resp.Error = vmNotFound(c.StringArgs["vm"]).Error()
		return resp
	}
	kvm, ok := vm.(*KvmVM)
	if !ok {
		resp.Error = fmt.Sprintf("`%s` is not a kvm vm -- command unsupported", vm.GetName())
		return resp
	}

	if c.BoolArgs["add"] {
		// generate an id by adding 1 to the highest in the list for the
		// hotplug devices, 0 if it's empty
		id := 0
		for k, _ := range kvm.hotplug {
			if k >= id {
				id = k + 1
			}
		}
		hid := fmt.Sprintf("hotplug%v", id)
		log.Debugln("hotplug generated id:", hid)

		r, err := kvm.q.DriveAdd(hid, c.StringArgs["filename"])
		if err != nil {
			resp.Error = err.Error()
			return resp
		}

		log.Debugln("hotplug drive_add response:", r)
		r, err = kvm.q.USBDeviceAdd(hid)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}

		log.Debugln("hotplug usb device add response:", r)
		kvm.hotplug[id] = c.StringArgs["filename"]
	} else if c.BoolArgs["remove"] {
		if c.StringArgs["disk"] == Wildcard {
			for k := range kvm.hotplug {
				if err := kvm.hotplugRemove(k); err != nil {
					resp.Error = err.Error()
					// TODO: try to remove the rest if there's an error?
					break
				}
			}

			return resp
		}

		id, err := strconv.Atoi(c.StringArgs["disk"])
		if err != nil {
			resp.Error = err.Error()
		} else if err := kvm.hotplugRemove(id); err != nil {
			resp.Error = err.Error()
		}
	} else if c.BoolArgs["show"] {
		if len(kvm.hotplug) > 0 {
			resp.Header = []string{"hotplug ID", "File"}
			resp.Tabular = [][]string{}

			for k, v := range kvm.hotplug {
				resp.Tabular = append(resp.Tabular, []string{strconv.Itoa(k), v})
			}
		}
	}

	return resp
}

func cliVmNetMod(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	vm := vms.findVm(c.StringArgs["vm"])
	if vm == nil {
		resp.Error = vmNotFound(c.StringArgs["vm"]).Error()
		return resp
	}

	pos, err := strconv.Atoi(c.StringArgs["tap"])
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	if c.BoolArgs["disconnect"] {
		err = vm.NetworkDisconnect(pos)
	} else {
		vlan := 0

		vlan, err = strconv.Atoi(c.StringArgs["vlan"])

		if vlan < 0 || vlan >= 4096 {
			err = fmt.Errorf("invalid vlan tag %v", vlan)
		}

		if err == nil {
			err = vm.NetworkConnect(pos, c.StringArgs["bridge"], vlan)
		}
	}

	if err != nil {
		resp.Error = err.Error()
	}

	return resp
}

// cliVMSuggest takes a prefix that could be the start of a VM name or a VM ID
// and makes suggestions for VM names (or IDs) that have a common prefix. mask
// can be used to only complete for VMs that are in a particular state (e.g.
// running). Returns a list of suggestions.
func cliVMSuggest(prefix string, mask VMState) []string {
	var isID bool
	res := []string{}

	if _, err := strconv.Atoi(prefix); err == nil {
		isID = true
	}

	for _, vm := range vms {
		if vm.GetState()&mask == 0 {
			continue
		}

		if isID {
			id := strconv.Itoa(vm.GetID())

			if strings.HasPrefix(id, prefix) {
				res = append(res, id)
			}
		} else if strings.HasPrefix(vm.GetName(), prefix) {
			res = append(res, vm.GetName())
		}
	}

	return res
}
