//go:build darwin && cgo && !server

#import <AppKit/AppKit.h>
#import <ImageIO/ImageIO.h>
#include <stdlib.h>
#include <string.h>
#include "reader.h"

static int copyClipboardImage(void **out, int *length) {
    @autoreleasepool {
        NSPasteboard *board = [NSPasteboard generalPasteboard];
        NSData *data = [board dataForType:NSPasteboardTypePNG];
        if (!data) {
            NSData *tiff = [board dataForType:NSPasteboardTypeTIFF];
            if (!tiff) return 0;
            if (tiff.length > (20 << 20)) return -1;
            CGImageSourceRef source = CGImageSourceCreateWithData((CFDataRef)tiff, NULL);
            if (!source) return -1;
            NSDictionary *properties = (NSDictionary *)CGImageSourceCopyPropertiesAtIndex(source, 0, NULL);
            long long width = [properties[(NSString *)kCGImagePropertyPixelWidth] longLongValue];
            long long height = [properties[(NSString *)kCGImagePropertyPixelHeight] longLongValue];
            [properties release];
            CFRelease(source);
            if (width <= 0 || height <= 0 || width > 64000000 / height) return -1;
            NSBitmapImageRep *bitmap = [NSBitmapImageRep imageRepWithData:tiff];
            if (!bitmap || bitmap.pixelsWide <= 0 || bitmap.pixelsHigh <= 0 ||
                bitmap.pixelsWide > 64000000 / bitmap.pixelsHigh) return -1;
            data = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        }
        if (!data || data.length == 0 || data.length > (20 << 20)) return -1;
        *out = malloc(data.length);
        if (!*out) return -1;
        memcpy(*out, data.bytes, data.length);
        *length = (int)data.length;
        return 1;
    }
}

HiveClipboardImage hiveClipboardImage(void) {
    HiveClipboardImage result = {0};
    result.status = copyClipboardImage(&result.data, &result.length);
    return result;
}
