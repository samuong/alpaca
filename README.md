# PAC Finder Darwin - Pull Request

## Changes Made

This PR includes two main changes to the `pacfinder_darwin.go` file:

1. **Fixed Boolean Type in CFStringGetBytes Call**
   - Changed `false` to `C.Boolean(0)` in the `CFStringGetBytes` function call
   - This resolves a compilation error where a Go boolean couldn't be used as a C boolean type
   - Error fixed: `cannot use false (untyped bool constant) as _Ctype_Boolean value in variable declaration`

2. **Removed Debug Logging**
   - Commented out all log statements in the code that were printing debug information
   - Modified the main function to print only the PAC URL without any log prefix
   - This makes the output cleaner and more suitable for programmatic consumption

## Why These Changes Are Needed

1. **Boolean Type Fix**
   - The code was failing to compile on some macOS systems due to the boolean type mismatch
   - This fix ensures compatibility across different Go versions and macOS environments

2. **Silent Operation**
   - The previous implementation printed verbose debug logs that weren't necessary for normal operation
   - The new implementation only outputs the PAC URL, making it easier to use in scripts or other programs

## Testing

The changes have been tested on macOS and confirmed to:
- Compile successfully without errors
- Run correctly and find the PAC URL using the appropriate method
- Output only the PAC URL without any debug messages

## Compatibility

These changes maintain compatibility with different macOS versions by preserving both methods of finding the PAC URL:
1. Using `SCDynamicStoreCopyValue` with specific key
2. Using `SCDynamicStoreCopyProxies`

This dual approach ensures the code works across various macOS versions where the ProxyURL might be stored in different C variables.
