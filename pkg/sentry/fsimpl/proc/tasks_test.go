// Copyright 2019 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proc

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"testing"

	"gvisor.dev/gvisor/pkg/abi/linux"
	"gvisor.dev/gvisor/pkg/context"
	"gvisor.dev/gvisor/pkg/fspath"
	"gvisor.dev/gvisor/pkg/sentry/fsimpl/testutil"
	"gvisor.dev/gvisor/pkg/sentry/kernel"
	"gvisor.dev/gvisor/pkg/sentry/kernel/auth"
	"gvisor.dev/gvisor/pkg/sentry/vfs"
	"gvisor.dev/gvisor/pkg/syserror"
	"gvisor.dev/gvisor/pkg/usermem"
)

var (
	// Next offset 256 by convention. Adds 1 for the next offset.
	selfLink       = vfs.Dirent{Type: linux.DT_LNK, NextOff: 256 + 0 + 1}
	threadSelfLink = vfs.Dirent{Type: linux.DT_LNK, NextOff: 256 + 1 + 1}

	// /proc/[pid] next offset starts at 256+2 (files above), then adds the
	// PID, and adds 1 for the next offset.
	proc1 = vfs.Dirent{Type: linux.DT_DIR, NextOff: 258 + 1 + 1}
	proc2 = vfs.Dirent{Type: linux.DT_DIR, NextOff: 258 + 2 + 1}
	proc3 = vfs.Dirent{Type: linux.DT_DIR, NextOff: 258 + 3 + 1}
)

var (
	tasksStaticFiles = map[string]testutil.DirentType{
		"cpuinfo":     linux.DT_REG,
		"loadavg":     linux.DT_REG,
		"meminfo":     linux.DT_REG,
		"mounts":      linux.DT_LNK,
		"net":         linux.DT_DIR,
		"self":        linux.DT_LNK,
		"stat":        linux.DT_REG,
		"sys":         linux.DT_DIR,
		"thread-self": linux.DT_LNK,
		"uptime":      linux.DT_REG,
		"version":     linux.DT_REG,
	}
	tasksStaticFilesNextOffs = map[string]int64{
		"self":        selfLink.NextOff,
		"thread-self": threadSelfLink.NextOff,
	}
	taskStaticFiles = map[string]testutil.DirentType{
		"auxv":    linux.DT_REG,
		"cgroup":  linux.DT_REG,
		"cmdline": linux.DT_REG,
		"comm":    linux.DT_REG,
		"environ": linux.DT_REG,
		"gid_map": linux.DT_REG,
		"io":      linux.DT_REG,
		"maps":    linux.DT_REG,
		"ns":      linux.DT_DIR,
		"smaps":   linux.DT_REG,
		"stat":    linux.DT_REG,
		"statm":   linux.DT_REG,
		"status":  linux.DT_REG,
		"task":    linux.DT_DIR,
		"uid_map": linux.DT_REG,
	}
)

func setup(t *testing.T) *testutil.System {
	k, err := testutil.Boot()
	if err != nil {
		t.Fatalf("Error creating kernel: %v", err)
	}

	ctx := k.SupervisorContext()
	creds := auth.CredentialsFromContext(ctx)

	k.VFS().MustRegisterFilesystemType(Name, &FilesystemType{}, &vfs.RegisterFilesystemTypeOptions{
		AllowUserMount: true,
	})
	fsOpts := vfs.GetFilesystemOptions{
		InternalData: &InternalData{
			Cgroups: map[string]string{
				"cpuset": "/foo/cpuset",
				"memory": "/foo/memory",
			},
		},
	}
	mntns, err := k.VFS().NewMountNamespace(ctx, creds, "", Name, &fsOpts)
	if err != nil {
		t.Fatalf("NewMountNamespace(): %v", err)
	}
	return testutil.NewSystem(ctx, t, k.VFS(), mntns)
}

func TestTasksEmpty(t *testing.T) {
	s := setup(t)
	defer s.Destroy()

	collector := s.ListDirents(s.PathOpAtRoot("/"))
	s.AssertAllDirentTypes(collector, tasksStaticFiles)
	s.AssertDirentOffsets(collector, tasksStaticFilesNextOffs)
}

func TestTasks(t *testing.T) {
	s := setup(t)
	defer s.Destroy()

	expectedDirents := make(map[string]testutil.DirentType)
	for n, d := range tasksStaticFiles {
		expectedDirents[n] = d
	}

	k := kernel.KernelFromContext(s.Ctx)
	var tasks []*kernel.Task
	for i := 0; i < 5; i++ {
		tc := k.NewThreadGroup(nil, k.RootPIDNamespace(), kernel.NewSignalHandlers(), linux.SIGCHLD, k.GlobalInit().Limits())
		task, err := testutil.CreateTask(s.Ctx, fmt.Sprintf("name-%d", i), tc, s.MntNs, s.Root, s.Root)
		if err != nil {
			t.Fatalf("CreateTask(): %v", err)
		}
		tasks = append(tasks, task)
		expectedDirents[fmt.Sprintf("%d", i+1)] = linux.DT_DIR
	}

	collector := s.ListDirents(s.PathOpAtRoot("/"))
	s.AssertAllDirentTypes(collector, expectedDirents)
	s.AssertDirentOffsets(collector, tasksStaticFilesNextOffs)

	lastPid := 0
	dirents := collector.OrderedDirents()
	doneSkippingNonTaskDirs := false
	for _, d := range dirents {
		pid, err := strconv.Atoi(d.Name)
		if err != nil {
			if !doneSkippingNonTaskDirs {
				// We haven't gotten to the task dirs yet.
				continue
			}
			t.Fatalf("Invalid process directory %q", d.Name)
		}
		doneSkippingNonTaskDirs = true
		if lastPid > pid {
			t.Errorf("pids not in order: %v", dirents)
		}
		found := false
		for _, t := range tasks {
			if k.TaskSet().Root.IDOfTask(t) == kernel.ThreadID(pid) {
				found = true
			}
		}
		if !found {
			t.Errorf("Additional task ID %d listed: %v", pid, tasks)
		}
		// Next offset starts at 256+2 ('self' and 'thread-self'), then adds the
		// PID, and adds 1 for the next offset.
		if want := int64(256 + 2 + pid + 1); d.NextOff != want {
			t.Errorf("Wrong dirent offset want: %d got: %d: %+v", want, d.NextOff, d)
		}
	}
	if !doneSkippingNonTaskDirs {
		t.Fatalf("Never found any process directories.")
	}

	// Test lookup.
	for _, path := range []string{"/1", "/2"} {
		fd, err := s.VFS.OpenAt(
			s.Ctx,
			s.Creds,
			s.PathOpAtRoot(path),
			&vfs.OpenOptions{},
		)
		if err != nil {
			t.Fatalf("vfsfs.OpenAt(%q) failed: %v", path, err)
		}
		buf := make([]byte, 1)
		bufIOSeq := usermem.BytesIOSequence(buf)
		if _, err := fd.Read(s.Ctx, bufIOSeq, vfs.ReadOptions{}); err != syserror.EISDIR {
			t.Errorf("wrong error reading directory: %v", err)
		}
	}

	if _, err := s.VFS.OpenAt(
		s.Ctx,
		s.Creds,
		s.PathOpAtRoot("/9999"),
		&vfs.OpenOptions{},
	); err != syserror.ENOENT {
		t.Fatalf("wrong error from vfsfs.OpenAt(/9999): %v", err)
	}
}

func TestTasksOffset(t *testing.T) {
	s := setup(t)
	defer s.Destroy()

	k := kernel.KernelFromContext(s.Ctx)
	for i := 0; i < 3; i++ {
		tc := k.NewThreadGroup(nil, k.RootPIDNamespace(), kernel.NewSignalHandlers(), linux.SIGCHLD, k.GlobalInit().Limits())
		if _, err := testutil.CreateTask(s.Ctx, fmt.Sprintf("name-%d", i), tc, s.MntNs, s.Root, s.Root); err != nil {
			t.Fatalf("CreateTask(): %v", err)
		}
	}

	for _, tc := range []struct {
		name   string
		offset int64
		wants  map[string]vfs.Dirent
	}{
		{
			name:   "small offset",
			offset: 100,
			wants: map[string]vfs.Dirent{
				"self":        selfLink,
				"thread-self": threadSelfLink,
				"1":           proc1,
				"2":           proc2,
				"3":           proc3,
			},
		},
		{
			name:   "offset at start",
			offset: 256,
			wants: map[string]vfs.Dirent{
				"self":        selfLink,
				"thread-self": threadSelfLink,
				"1":           proc1,
				"2":           proc2,
				"3":           proc3,
			},
		},
		{
			name:   "skip /proc/self",
			offset: 257,
			wants: map[string]vfs.Dirent{
				"thread-self": threadSelfLink,
				"1":           proc1,
				"2":           proc2,
				"3":           proc3,
			},
		},
		{
			name:   "skip symlinks",
			offset: 258,
			wants: map[string]vfs.Dirent{
				"1": proc1,
				"2": proc2,
				"3": proc3,
			},
		},
		{
			name:   "skip first process",
			offset: 260,
			wants: map[string]vfs.Dirent{
				"2": proc2,
				"3": proc3,
			},
		},
		{
			name:   "last process",
			offset: 261,
			wants: map[string]vfs.Dirent{
				"3": proc3,
			},
		},
		{
			name:   "after last",
			offset: 262,
			wants:  nil,
		},
		{
			name:   "TaskLimit+1",
			offset: kernel.TasksLimit + 1,
			wants:  nil,
		},
		{
			name:   "max",
			offset: math.MaxInt64,
			wants:  nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := s.WithSubtest(t)
			fd, err := s.VFS.OpenAt(
				s.Ctx,
				s.Creds,
				s.PathOpAtRoot("/"),
				&vfs.OpenOptions{},
			)
			if err != nil {
				t.Fatalf("vfsfs.OpenAt(/) failed: %v", err)
			}
			if _, err := fd.Seek(s.Ctx, tc.offset, linux.SEEK_SET); err != nil {
				t.Fatalf("Seek(%d, SEEK_SET): %v", tc.offset, err)
			}

			var collector testutil.DirentCollector
			if err := fd.IterDirents(s.Ctx, &collector); err != nil {
				t.Fatalf("IterDirent(): %v", err)
			}

			expectedTypes := make(map[string]testutil.DirentType)
			expectedOffsets := make(map[string]int64)
			for name, want := range tc.wants {
				expectedTypes[name] = want.Type
				if want.NextOff != 0 {
					expectedOffsets[name] = want.NextOff
				}
			}

			collector.SkipDotsChecks(true) // We seek()ed past the dots.
			s.AssertAllDirentTypes(&collector, expectedTypes)
			s.AssertDirentOffsets(&collector, expectedOffsets)
		})
	}
}

func TestTask(t *testing.T) {
	s := setup(t)
	defer s.Destroy()

	k := kernel.KernelFromContext(s.Ctx)
	tc := k.NewThreadGroup(nil, k.RootPIDNamespace(), kernel.NewSignalHandlers(), linux.SIGCHLD, k.GlobalInit().Limits())
	_, err := testutil.CreateTask(s.Ctx, "name", tc, s.MntNs, s.Root, s.Root)
	if err != nil {
		t.Fatalf("CreateTask(): %v", err)
	}

	collector := s.ListDirents(s.PathOpAtRoot("/1"))
	s.AssertAllDirentTypes(collector, taskStaticFiles)
}

func TestProcSelf(t *testing.T) {
	s := setup(t)
	defer s.Destroy()

	k := kernel.KernelFromContext(s.Ctx)
	tc := k.NewThreadGroup(nil, k.RootPIDNamespace(), kernel.NewSignalHandlers(), linux.SIGCHLD, k.GlobalInit().Limits())
	task, err := testutil.CreateTask(s.Ctx, "name", tc, s.MntNs, s.Root, s.Root)
	if err != nil {
		t.Fatalf("CreateTask(): %v", err)
	}

	collector := s.WithTemporaryContext(task).ListDirents(&vfs.PathOperation{
		Root:               s.Root,
		Start:              s.Root,
		Path:               fspath.Parse("/self/"),
		FollowFinalSymlink: true,
	})
	s.AssertAllDirentTypes(collector, taskStaticFiles)
}

func iterateDir(ctx context.Context, t *testing.T, s *testutil.System, fd *vfs.FileDescription) {
	t.Logf("Iterating: /proc%s", fd.MappedName(ctx))

	var collector testutil.DirentCollector
	if err := fd.IterDirents(ctx, &collector); err != nil {
		t.Fatalf("IterDirents(): %v", err)
	}
	if err := collector.Contains(".", linux.DT_DIR); err != nil {
		t.Error(err.Error())
	}
	if err := collector.Contains("..", linux.DT_DIR); err != nil {
		t.Error(err.Error())
	}

	for _, d := range collector.Dirents() {
		if d.Name == "." || d.Name == ".." {
			continue
		}
		childPath := path.Join(fd.MappedName(ctx), d.Name)
		if d.Type == linux.DT_LNK {
			link, err := s.VFS.ReadlinkAt(
				ctx,
				auth.CredentialsFromContext(ctx),
				&vfs.PathOperation{Root: s.Root, Start: s.Root, Path: fspath.Parse(childPath)},
			)
			if err != nil {
				t.Errorf("vfsfs.ReadlinkAt(%v) failed: %v", childPath, err)
			} else {
				t.Logf("Skipping symlink: /proc%s => %s", childPath, link)
			}
			continue
		}

		t.Logf("Opening: /proc%s", childPath)
		child, err := s.VFS.OpenAt(
			ctx,
			auth.CredentialsFromContext(ctx),
			&vfs.PathOperation{Root: s.Root, Start: s.Root, Path: fspath.Parse(childPath)},
			&vfs.OpenOptions{},
		)
		if err != nil {
			t.Errorf("vfsfs.OpenAt(%v) failed: %v", childPath, err)
			continue
		}
		stat, err := child.Stat(ctx, vfs.StatOptions{})
		if err != nil {
			t.Errorf("Stat(%v) failed: %v", childPath, err)
		}
		if got := linux.FileMode(stat.Mode).DirentType(); got != d.Type {
			t.Errorf("wrong file mode, stat: %v, dirent: %v", got, d.Type)
		}
		if d.Type == linux.DT_DIR {
			// Found another dir, let's do it again!
			iterateDir(ctx, t, s, child)
		}
	}
}

// TestTree iterates all directories and stats every file.
func TestTree(t *testing.T) {
	s := setup(t)
	defer s.Destroy()

	k := kernel.KernelFromContext(s.Ctx)
	var tasks []*kernel.Task
	for i := 0; i < 5; i++ {
		tc := k.NewThreadGroup(nil, k.RootPIDNamespace(), kernel.NewSignalHandlers(), linux.SIGCHLD, k.GlobalInit().Limits())
		task, err := testutil.CreateTask(s.Ctx, fmt.Sprintf("name-%d", i), tc, s.MntNs, s.Root, s.Root)
		if err != nil {
			t.Fatalf("CreateTask(): %v", err)
		}
		tasks = append(tasks, task)
	}

	ctx := tasks[0]
	fd, err := s.VFS.OpenAt(
		ctx,
		auth.CredentialsFromContext(s.Ctx),
		&vfs.PathOperation{Root: s.Root, Start: s.Root, Path: fspath.Parse("/")},
		&vfs.OpenOptions{},
	)
	if err != nil {
		t.Fatalf("vfsfs.OpenAt(/) failed: %v", err)
	}
	iterateDir(ctx, t, s, fd)
}
