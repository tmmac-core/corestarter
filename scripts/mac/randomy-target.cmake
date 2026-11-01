# Included by scripts/build-mac.sh right after RandomY's project(): RandomY sets
# CMAKE_OSX_DEPLOYMENT_TARGET to 10.12 further down, which the current libc++
# headers no longer support. The deferred call runs after the whole file, so
# the target is ours (MACOSX_DEPLOYMENT_TARGET) when the build files are written.
cmake_language(DEFER CALL set CMAKE_OSX_DEPLOYMENT_TARGET "$ENV{MACOSX_DEPLOYMENT_TARGET}")
