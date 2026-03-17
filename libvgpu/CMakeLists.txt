cmake_minimum_required(VERSION 3.18)
project(libvgpu C)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -Wall -Wextra -fPIC")

# Find CUDA (optional — skip gracefully if not found)
find_package(CUDAToolkit QUIET)

include_directories(${CMAKE_CURRENT_SOURCE_DIR}/include)

set(SOURCES
    src/hook.c
    src/cuda_hook.c
    src/nvml_hook.c
    src/phoenix_meter.c
)

add_library(vgpu SHARED ${SOURCES})
target_link_libraries(vgpu dl pthread)

if(CUDAToolkit_FOUND)
    message(STATUS "CUDA found: ${CUDAToolkit_VERSION}")
    target_include_directories(vgpu PRIVATE ${CUDAToolkit_INCLUDE_DIRS})
    target_compile_definitions(vgpu PRIVATE PHOENIX_CUDA_AVAILABLE)
else()
    message(WARNING "CUDA toolkit not found — building stub mode (no GPU interception)")
    target_compile_definitions(vgpu PRIVATE PHOENIX_STUB_MODE)
endif()

set_target_properties(vgpu PROPERTIES
    OUTPUT_NAME "vgpu"
    SOVERSION 1
    VERSION 0.1.0
)

install(TARGETS vgpu LIBRARY DESTINATION lib)
install(FILES include/libvgpu.h DESTINATION include)
