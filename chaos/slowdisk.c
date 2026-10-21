#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <stdio.h>
#include <time.h>
#include <unistd.h>

// Test-only syscall latency injection. Every delayed call still executes real fsync.
// Activated by a file inside an isolated Compose volume; never enabled in normal Compose.
static void delay_sync(const char *call) {
  int saved = errno;
  if (access("/faults/slow-disk", F_OK) == 0) {
    struct timespec wait = {.tv_sec = 0, .tv_nsec = 150000000};
    while (nanosleep(&wait, &wait) == -1 && errno == EINTR) {}
    dprintf(STDERR_FILENO, "{\"fault\":\"slow_storage_sync\",\"call\":\"%s\",\"injected_ms\":150}\n", call);
  }
  errno = saved;
}
int fsync(int fd) {
  int (*real_sync)(int) = dlsym(RTLD_NEXT, "fsync");
  if (!real_sync) {errno = ENOSYS;return -1;}
  delay_sync("fsync");return real_sync(fd);
}
int fdatasync(int fd) {
  int (*real_sync)(int) = dlsym(RTLD_NEXT, "fdatasync");
  if (!real_sync) {errno = ENOSYS;return -1;}
  delay_sync("fdatasync");return real_sync(fd);
}
