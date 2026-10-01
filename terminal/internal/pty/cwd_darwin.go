//go:build cgo

package pty

/*
#include <libproc.h>
#include <sys/proc_info.h>
*/
import "C"

import "unsafe"

// ProcessCWD returns a process's working directory. macOS only exposes
// this through libproc.
func ProcessCWD(pid int) string {
	var info C.struct_proc_vnodepathinfo
	size := C.int(unsafe.Sizeof(info))
	if C.proc_pidinfo(C.int(pid), C.PROC_PIDVNODEPATHINFO, 0, unsafe.Pointer(&info), size) != size {
		return ""
	}
	return C.GoString(&info.pvi_cdir.vip_path[0])
}
