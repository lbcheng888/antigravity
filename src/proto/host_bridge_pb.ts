// Defines the contract between the language server and the application hosting
// it. The host serves this over a loopback Connect listener and the language
// server calls it, so that the language server can ask the host about things
// only the host knows.
//
// This is deliberately separate from language_server.proto: a host is not
// otherwise a language server client, and should not have to depend on that
// much larger contract to answer these questions.
//
// This file is a copy. The source of truth lives in google3 at
// cs/third_party/jetski/host_bridge_pb/host_bridge.proto, and the two must be
// kept in step by hand: this repository is not part of that export.

import type { Message } from '@bufbuild/protobuf';
import type { GenFile, GenMessage, GenService } from '@bufbuild/protobuf/codegenv2';
import { fileDesc, messageDesc, serviceDesc } from '@bufbuild/protobuf/codegenv2';

/**
 * Describes the file host_bridge.proto.
 */
export const file_host_bridge: GenFile = fileDesc(
  'ChFob3N0X2JyaWRnZS5wcm90bxISZXhhLmhvc3RfYnJpZGdlX3BiIhgKFkdldFVwZGF0ZVN0YXR1c1JlcXVlc3QiSwoXR2V0VXBkYXRlU3RhdHVzUmVzcG9uc2USMAoGc3RhdHVzGAEgASgLMiAuZXhhLmhvc3RfYnJpZGdlX3BiLlVwZGF0ZVN0YXR1cyJZCgxVcGRhdGVTdGF0dXMSFwoPY3VycmVudF92ZXJzaW9uGAEgASgJEhYKDmxhdGVzdF92ZXJzaW9uGAIgASgJEhgKEHVwZGF0ZV9hdmFpbGFibGUYAyABKAgiFAoSQXBwbHlVcGRhdGVSZXF1ZXN0IicKE0FwcGx5VXBkYXRlUmVzcG9uc2USEAoIYWNjZXB0ZWQYASABKAgy3wEKEUhvc3RCcmlkZ2VTZXJ2aWNlEmoKD0dldFVwZGF0ZVN0YXR1cxIqLmV4YS5ob3N0X2JyaWRnZV9wYi5HZXRVcGRhdGVTdGF0dXNSZXF1ZXN0GisuZXhhLmhvc3RfYnJpZGdlX3BiLkdldFVwZGF0ZVN0YXR1c1Jlc3BvbnNlEl4KC0FwcGx5VXBkYXRlEiYuZXhhLmhvc3RfYnJpZGdlX3BiLkFwcGx5VXBkYXRlUmVxdWVzdBonLmV4YS5ob3N0X2JyaWRnZV9wYi5BcHBseVVwZGF0ZVJlc3BvbnNlYgZwcm90bzM',
);

/**
 * @generated from message exa.host_bridge_pb.GetUpdateStatusRequest
 */
export type GetUpdateStatusRequest = Message<'exa.host_bridge_pb.GetUpdateStatusRequest'> & {};

/**
 * Describes the message exa.host_bridge_pb.GetUpdateStatusRequest.
 * Use `create(GetUpdateStatusRequestSchema)` to create a new message.
 */
export const GetUpdateStatusRequestSchema: GenMessage<GetUpdateStatusRequest> =
  messageDesc<GetUpdateStatusRequest>(file_host_bridge, 0);

/**
 * @generated from message exa.host_bridge_pb.GetUpdateStatusResponse
 */
export type GetUpdateStatusResponse = Message<'exa.host_bridge_pb.GetUpdateStatusResponse'> & {
  /**
   * @generated from field: exa.host_bridge_pb.UpdateStatus status = 1;
   */
  status?: UpdateStatus;
};

/**
 * Describes the message exa.host_bridge_pb.GetUpdateStatusResponse.
 * Use `create(GetUpdateStatusResponseSchema)` to create a new message.
 */
export const GetUpdateStatusResponseSchema: GenMessage<GetUpdateStatusResponse> =
  messageDesc<GetUpdateStatusResponse>(file_host_bridge, 1);

/**
 * @generated from message exa.host_bridge_pb.UpdateStatus
 */
export type UpdateStatus = Message<'exa.host_bridge_pb.UpdateStatus'> & {
  /**
   * @generated from field: string current_version = 1;
   */
  currentVersion: string;
  /**
   * @generated from field: string latest_version = 2;
   */
  latestVersion: string;
  /**
   * @generated from field: bool update_available = 3;
   */
  updateAvailable: boolean;
};

/**
 * Describes the message exa.host_bridge_pb.UpdateStatus.
 * Use `create(UpdateStatusSchema)` to create a new message.
 */
export const UpdateStatusSchema: GenMessage<UpdateStatus> =
  messageDesc<UpdateStatus>(file_host_bridge, 2);

/**
 * @generated from message exa.host_bridge_pb.ApplyUpdateRequest
 */
export type ApplyUpdateRequest = Message<'exa.host_bridge_pb.ApplyUpdateRequest'> & {};

/**
 * Describes the message exa.host_bridge_pb.ApplyUpdateRequest.
 * Use `create(ApplyUpdateRequestSchema)` to create a new message.
 */
export const ApplyUpdateRequestSchema: GenMessage<ApplyUpdateRequest> =
  messageDesc<ApplyUpdateRequest>(file_host_bridge, 3);

/**
 * @generated from message exa.host_bridge_pb.ApplyUpdateResponse
 */
export type ApplyUpdateResponse = Message<'exa.host_bridge_pb.ApplyUpdateResponse'> & {
  /**
   * @generated from field: bool accepted = 1;
   */
  accepted: boolean;
};

/**
 * Describes the message exa.host_bridge_pb.ApplyUpdateResponse.
 * Use `create(ApplyUpdateResponseSchema)` to create a new message.
 */
export const ApplyUpdateResponseSchema: GenMessage<ApplyUpdateResponse> =
  messageDesc<ApplyUpdateResponse>(file_host_bridge, 4);

/**
 * @generated from service exa.host_bridge_pb.HostBridgeService
 */
export const HostBridgeService: GenService<{
  /**
   * @generated from rpc exa.host_bridge_pb.HostBridgeService.GetUpdateStatus
   */
  getUpdateStatus: {
    methodKind: 'unary';
    input: typeof GetUpdateStatusRequestSchema;
    output: typeof GetUpdateStatusResponseSchema;
  };
  /**
   * @generated from rpc exa.host_bridge_pb.HostBridgeService.ApplyUpdate
   */
  applyUpdate: {
    methodKind: 'unary';
    input: typeof ApplyUpdateRequestSchema;
    output: typeof ApplyUpdateResponseSchema;
  };
}> = serviceDesc<{
  getUpdateStatus: {
    methodKind: 'unary';
    input: typeof GetUpdateStatusRequestSchema;
    output: typeof GetUpdateStatusResponseSchema;
  };
  applyUpdate: {
    methodKind: 'unary';
    input: typeof ApplyUpdateRequestSchema;
    output: typeof ApplyUpdateResponseSchema;
  };
}>(file_host_bridge, 0);
