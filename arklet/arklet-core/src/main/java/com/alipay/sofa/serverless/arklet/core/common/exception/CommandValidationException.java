package com.alipay.sofa.serverless.arklet.core.common.exception;

/**
 * @author mingmen
 * @date 2023/6/14
 */
public class CommandValidationException extends ArkletException {
    public CommandValidationException(String message) {
        super(message);
    }
}
