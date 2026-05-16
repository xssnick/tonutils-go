
#ifndef EMULATOR_EXPORT_H
#define EMULATOR_EXPORT_H

#ifdef EMULATOR_STATIC_DEFINE
#  define EMULATOR_EXPORT
#  define EMULATOR_NO_EXPORT
#else
#  ifndef EMULATOR_EXPORT
#    ifdef emulator_EXPORTS
        /* We are building this library */
#      define EMULATOR_EXPORT __attribute__((visibility("default")))
#    else
        /* We are using this library */
#      define EMULATOR_EXPORT __attribute__((visibility("default")))
#    endif
#  endif

#  ifndef EMULATOR_NO_EXPORT
#    define EMULATOR_NO_EXPORT __attribute__((visibility("hidden")))
#  endif
#endif

#ifndef EMULATOR_DEPRECATED
#  define EMULATOR_DEPRECATED __attribute__ ((__deprecated__))
#endif

#ifndef EMULATOR_DEPRECATED_EXPORT
#  define EMULATOR_DEPRECATED_EXPORT EMULATOR_EXPORT EMULATOR_DEPRECATED
#endif

#ifndef EMULATOR_DEPRECATED_NO_EXPORT
#  define EMULATOR_DEPRECATED_NO_EXPORT EMULATOR_NO_EXPORT EMULATOR_DEPRECATED
#endif

#if 0 /* DEFINE_NO_DEPRECATED */
#  ifndef EMULATOR_NO_DEPRECATED
#    define EMULATOR_NO_DEPRECATED
#  endif
#endif

#endif /* EMULATOR_EXPORT_H */
