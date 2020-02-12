"""Wrappers for common build rules.

These wrappers apply common BUILD configurations (e.g., proto_library
automagically creating cc_ and go_ proto targets) and act as a single point of
change for Google-internal and bazel-compatible rules.
"""

load("//tools/go_stateify:defs.bzl", "go_stateify")
load("//tools/go_marshal:defs.bzl", "go_marshal", "marshal_deps", "marshal_test_deps")
load("//tools/bazeldefs:defs.bzl", "go_suffixes", _cc_binary = "cc_binary", _cc_flags_supplier = "cc_flags_supplier", _cc_library = "cc_library", _cc_proto_library = "cc_proto_library", _cc_test = "cc_test", _cc_toolchain = "cc_toolchain", _container_image = "container_image", _default_installer = "default_installer", _default_net_util = "default_net_util", _go_binary = "go_binary", _go_embed_data = "go_embed_data", _go_image = "go_image", _go_library = "go_library", _go_proto_library = "go_proto_library", _go_test = "go_test", _go_tool_library = "go_tool_library", _gtest = "gtest", _loopback = "loopback", _pkg_deb = "pkg_deb", _pkg_tar = "pkg_tar", _proto_library = "proto_library", _py_binary = "py_binary", _py_library = "py_library", _py_requirement = "py_requirement", _py_test = "py_test", _runsc_platforms = "runsc_platforms", _select_arch = "select_arch", _select_system = "select_system")

# Delegate directly.
cc_binary = _cc_binary
cc_library = _cc_library
cc_test = _cc_test
cc_toolchain = _cc_toolchain
cc_flags_supplier = _cc_flags_supplier
container_image = _container_image
go_embed_data = _go_embed_data
go_image = _go_image
go_test = _go_test
go_tool_library = _go_tool_library
gtest = _gtest
pkg_deb = _pkg_deb
pkg_tar = _pkg_tar
py_library = _py_library
py_binary = _py_binary
py_test = _py_test
py_requirement = _py_requirement
runsc_platforms = _runsc_platforms
select_arch = _select_arch
select_system = _select_system
loopback = _loopback
default_installer = _default_installer
default_net_util = _default_net_util

def go_binary(name, **kwargs):
    """Wraps the standard go_binary.

    Args:
      name: the rule name.
      **kwargs: standard go_binary arguments.
    """
    _go_binary(
        name = name,
        **kwargs
    )

def calculate_sets(srcs):
    """Calculates special Go sets for templates.

    Args:
      srcs: the full set of Go sources.

    Returns:
      A dictionary of the form:

      "": [src1.go, src2.go]
      "suffix": [src3suffix.go, src4suffix.go]

      Note that suffix will typically start with '_'.
    """
    result = dict()
    for file in srcs:
        if not file.endswith(".go"):
            continue
        target = ""
        for suffix in go_suffixes:
            if file.endswith(suffix + ".go"):
                target = suffix
        if not target in result:
            result[target] = [file]
        else:
            result[target].append(file)
    return result

def go_imports(name, src, out):
    """Simplify a single Go source file by eliminating unused imports."""
    native.genrule(
        name = name,
        srcs = [src],
        outs = [out],
        tools = ["@org_golang_x_tools//cmd/goimports:goimports"],
        cmd = ("$(location @org_golang_x_tools//cmd/goimports:goimports) $(SRCS) > $@"),
    )

def go_library(name, srcs, deps = [], imports = [], stateify = True, marshal = False, **kwargs):
    """Wraps the standard go_library and does stateification and marshalling.

    The recommended way is to use this rule with mostly identical configuration as the native
    go_library rule.

    These definitions provide additional flags (stateify, marshal) that can be used
    with the generators to automatically supplement the library code.

    load("//tools:defs.bzl", "go_library")

    go_library(
        name = "foo",
        srcs = ["foo.go"],
    )

    Args:
      name: the rule name.
      srcs: the library sources.
      deps: the library dependencies.
      imports: imports required for stateify.
      stateify: whether statify is enabled (default: true).
      marshal: whether marshal is enabled (default: false).
      **kwargs: standard go_library arguments.
    """
    all_srcs = srcs
    all_deps = deps
    dirname, _, _ = native.package_name().rpartition("/")
    full_pkg = dirname + "/" + name
    if stateify:
        # Only do stateification for non-state packages without manual autogen.
        # First, we need to segregate the input files via the special suffixes,
        # and calculate the final output set.
        state_sets = calculate_sets(srcs)
        for (suffix, srcs) in state_sets.items():
            go_stateify(
                name = name + suffix + "_state_autogen_with_imports",
                srcs = srcs,
                imports = imports,
                package = full_pkg,
                out = name + suffix + "_state_autogen_with_imports.go",
            )
            go_imports(
                name = name + suffix + "_state_autogen",
                src = name + suffix + "_state_autogen_with_imports.go",
                out = name + suffix + "_state_autogen.go",
            )
        all_srcs = all_srcs + [
            name + suffix + "_state_autogen.go"
            for suffix in state_sets.keys()
        ]
        if "//pkg/state" not in all_deps:
            all_deps = all_deps + ["//pkg/state"]

    if marshal:
        # See above.
        marshal_sets = calculate_sets(srcs)
        for (suffix, srcs) in marshal_sets.items():
            go_marshal(
                name = name + suffix + "_abi_autogen",
                srcs = srcs,
                debug = False,
                imports = imports,
                package = name,
            )
        extra_deps = [
            dep
            for dep in marshal_deps
            if not dep in all_deps
        ]
        all_deps = all_deps + extra_deps
        all_srcs = all_srcs + [
            name + suffix + "_abi_autogen_unsafe.go"
            for suffix in marshal_sets.keys()
        ]

    _go_library(
        name = name,
        srcs = all_srcs,
        deps = all_deps,
        **kwargs
    )

    if marshal:
        # Ignore importpath for go_test.
        kwargs.pop("importpath", None)

        # See above.
        marshal_sets = calculate_sets(srcs)
        for (suffix, srcs) in marshal_sets.items():
            _go_test(
                name = name + suffix + "_abi_autogen_test",
                srcs = [name + suffix + "_abi_autogen_test.go"],
                library = ":" + name + suffix,
                deps = marshal_test_deps,
                **kwargs
            )

def proto_library(name, srcs, **kwargs):
    """Wraps the standard proto_library.

    Given a proto_library named "foo", this produces three different targets:
    - foo_proto: proto_library rule.
    - foo_go_proto: go_proto_library rule.
    - foo_cc_proto: cc_proto_library rule.

    Args:
      srcs: the proto sources.
      **kwargs: standard proto_library arguments.
    """
    deps = kwargs.pop("deps", [])
    _proto_library(
        name = name + "_proto",
        srcs = srcs,
        deps = deps,
        **kwargs
    )
    _go_proto_library(
        name = name + "_go_proto",
        proto = ":" + name + "_proto",
        deps = deps,
        **kwargs
    )
    _cc_proto_library(
        name = name + "_cc_proto",
        deps = [":" + name + "_proto"],
        **kwargs
    )
