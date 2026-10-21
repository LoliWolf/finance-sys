// Native application identity + Network.framework permission request for launchd.
// Business configuration and all application logic remain in the Go API.
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#import <Network/Network.h>
#include <signal.h>

static NSTask *apiTask;
static nw_connection_t connection;
static BOOL started;
static BOOL stopping;

static void startAPI(void) {
    if (started || stopping) return;
    started = YES;
    NSString *path = [[NSBundle mainBundle].executablePath.stringByDeletingLastPathComponent
                      stringByAppendingPathComponent:@"finance-sys-api"];
    apiTask = [[NSTask alloc] init];
    apiTask.executableURL = [NSURL fileURLWithPath:path];
    apiTask.environment = [[NSProcessInfo processInfo] environment];
    apiTask.terminationHandler = ^(NSTask *task) {
        int status = task.terminationStatus;
        dispatch_async(dispatch_get_main_queue(), ^{ exit(stopping ? 0 : status); });
    };
    NSError *error = nil;
    if (![apiTask launchAndReturnError:&error]) {
        fprintf(stderr, "[launcher] Cannot start Go API: %s\n", error.localizedDescription.UTF8String);
        exit(1);
    }
    fprintf(stderr, "[launcher] Local network reachable; Go API PID %d\n", apiTask.processIdentifier);
}

int main(void) {
    @autoreleasepool {
        NSApplication *application = [NSApplication sharedApplication];
        [application setActivationPolicy:NSApplicationActivationPolicyAccessory];
        NSDictionary *environment = [[NSProcessInfo processInfo] environment];
        NSString *address = [environment[@"NACOS_SERVER_ADDR"] componentsSeparatedByString:@","].firstObject;
        NSURL *endpoint = [NSURL URLWithString:[@"http://" stringByAppendingString:address ?: @""]];
        if (!endpoint.host.length || !endpoint.port || ![environment[@"FINANCE_SYS_ENV"] isEqualToString:@"PROD"]) {
            fprintf(stderr, "[launcher] Start with start_prod.py; production Nacos bootstrap environment is required.\n");
            return 1;
        }

        signal(SIGTERM, SIG_IGN);
        signal(SIGINT, SIG_IGN);
        NSMutableArray *signals = [[NSMutableArray alloc] init];
        for (NSNumber *number in @[@(SIGTERM), @(SIGINT)]) {
            dispatch_source_t source = dispatch_source_create(DISPATCH_SOURCE_TYPE_SIGNAL, number.intValue, 0, dispatch_get_main_queue());
            dispatch_source_set_event_handler(source, ^{
                stopping = YES;
                if (connection) nw_connection_cancel(connection);
                if (apiTask.running) [apiTask terminate];
                else exit(0);
            });
            [signals addObject:source];
            dispatch_resume(source);
        }

        nw_endpoint_t remote = nw_endpoint_create_host(endpoint.host.UTF8String, endpoint.port.stringValue.UTF8String);
        nw_parameters_t parameters = nw_parameters_create_secure_tcp(NW_PARAMETERS_DISABLE_PROTOCOL, NW_PARAMETERS_DEFAULT_CONFIGURATION);
        connection = nw_connection_create(remote, parameters);
        nw_connection_set_queue(connection, dispatch_get_main_queue());
        nw_connection_set_state_changed_handler(connection, ^(nw_connection_state_t state, nw_error_t error) {
            if (state == nw_connection_state_ready) {
                nw_connection_cancel(connection);
                startAPI();
            } else if (state == nw_connection_state_waiting || state == nw_connection_state_failed) {
                fprintf(stderr, "[launcher] Waiting for local network authorization/reachability (state=%d, error=%d).\n",
                        (int)state, error ? nw_error_get_error_code(error) : 0);
            }
        });
        fprintf(stderr, "[launcher] Requesting local network access for FinanceSys via Network.framework.\n");
        nw_connection_start(connection);
        [application run];
        (void)signals;
    }
    return 0;
}
