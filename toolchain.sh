#!/bin/sh -e

cat <<EOF > toolchain.cmake
set(CMAKE_SYSTEM_NAME Linux)
set(CMAKE_SYSTEM_PROCESSOR $(xx-info march))
set(CMAKE_CXX_COMPILER clang++)
set(CMAKE_ASM_COMPILER clang)
set(CMAKE_C_COMPILER_TARGET $(xx-info))
set(CMAKE_CXX_COMPILER_TARGET $(xx-info))
set(CMAKE_ASM_COMPILER_TARGET $(xx-info))
EOF
