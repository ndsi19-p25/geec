cmake_minimum_required(VERSION 3.5)
project(node C)

set(CMAKE_C_STANDARD 99)

set(THREADS_PREFER_PTHREAD_FLAG ON)
find_package(Threads REQUIRED)


add_library(node node.h node.c)

target_link_libraries(node Threads::Threads)
