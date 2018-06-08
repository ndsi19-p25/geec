//
// Created by Michael Xusheng Chen on 11/4/2018.
//
#include "node.h"
#include <stdint.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>
#include <sys/socket.h>
#include <arpa/inet.h>
#include <string.h>
#include <unistd.h>
#include <sys/time.h>
#include <errno.h>

#define DEBUG 1
#define debug_print(fmt, ...) \
            do { if (DEBUG) fprintf(stderr, fmt, __VA_ARGS__); } while (0)

#define INFO 1
#define info_print(fmt, ...) \
            do { if (INFO) fprintf(stderr, fmt, __VA_ARGS__); } while (0)

#define BUFLEN 1024
#define MSG_LEN 49

#define ELEC_PREPARE 1
#define ELEC_PREPARED 2
#define ELEC_CONFIRM 3
#define ELEC_CONFIRMED 4
#define ELEC_ANNOUNCE 5  //only for debug usage.
#define ELEC_NOTIFY 6 //Notify a proposer about the history before.



struct Message{
    uint64_t term_start; 
    uint64_t rand;
    uint64_t blockNum;
    uint8_t message_type;
    uint32_t sender_idx;    //The msg is for whom.
    char addr[20];    //Sender of the msg.
};
typedef struct Message Message;


static void *RecvFunc(void *opaque);
static char* serialize(const Message *msg);
static Message deserialize(char *input);
static int broadcast(const Message *msg,Term_t *term);
static int insert_addr(char **addr_array, const char *addr,  int *count);
static void init_instance(Term_t *term, instance_t *instance);
static int send_to_member(int index, const Message* msg, Term_t *term);
static void free_instance(Term_t *term, instance_t *instance);

char **makeCharArray(int size) {
        return calloc(sizeof(char*), size);
}

void setArrayString(char **a, char *s, int n) {
        a[n] = s;
}

void freeCharArray(char **a, int size) {
        int i;
        for (i = 0; i < size; i++)
                free(a[i]);
        free(a);
}

// Term_t* New_Node(int offset, uint64_t start_blk, uint64_t len){ //currently for hardcoded message.
Term_t* New_Node(char **ipstrs, int *ports, int offset, char *my_account, int committee_count, uint64_t start_blk, uint64_t term_len){


    debug_print("Creating node with %d nodes:\n", committee_count);
    int i; 
    for (i = 0; i<committee_count; i++) {
        debug_print("     node: %d, ip: %s, port: %d\n", i, ipstrs[i], ports[i]);
    }

    time_t t;
    srand((unsigned) time(&t) + offset * 10000);
    
    
    
    Term_t* term;
    term = (Term_t*)malloc(sizeof(Term_t));
    term->my_idx = (uint32_t)offset;
    term->start_block = start_blk;
    term->len = term_len;
    //hard_coded;
    
    term->member_count = committee_count;
    term->members = (struct sockaddr_in *)malloc(committee_count * sizeof(struct sockaddr_in));
    uint32_t x;
    for (x =0; x<committee_count; x++){
        printf("ip : %s\n", ipstrs[x]);

        memset(&term->members[x], 0, sizeof(struct sockaddr_in));
        term->members[x].sin_family = AF_INET;
        term->members[x].sin_port = htons(ports[x]);
        term->members[x].sin_addr.s_addr = inet_addr(ipstrs[x]);
    }
    
    memcpy(term->my_account, my_account, 21);


    term->instances = (instance_t *) malloc(term->len * sizeof(instance_t));
    for (i = 0; i < term->len; i++){
        init_instance(term, &term->instances[i]);
    }

    term->sock=socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP);
    if (term->sock < 0){
        perror("Failed to create socket\n");
        pthread_exit(NULL);
    }
    if (bind(term->sock, (struct sockaddr*)&term->members[offset], sizeof(term->members[offset])) == -1){
        perror("Failed to bind to socket\n");
        pthread_exit(NULL);
    }


    struct timeval tv; 
    tv.tv_sec = 0;
    tv.tv_usec = 100000;
    if (setsockopt(term->sock, SOL_SOCKET, SO_RCVTIMEO,&tv,sizeof(tv)) < 0) {
        perror("Error");
    }


    term->should_stop = 0;
    pthread_mutex_init(&term->flag_lock, NULL);


    int ret;
    ret = pthread_create(&term->recvt, NULL, RecvFunc, (void *)term);
    if (ret != 0){
        perror("Failed to create thread");
    }

    return term;
}


void handle_prepare(const Message *msg, const struct sockaddr_in *si_other, Term_t *term, socklen_t si_len){
    uint64_t offset = msg->blockNum - term->start_block;
    if (offset >= term->len){
        debug_print("Message NOT for my term, start=%lu, len = %lu, blk_num = %lu\n", term->start_block, term->len,  msg->blockNum);
        return;
    }
    instance_t *instance = &term->instances[offset];
    debug_print("[%d] Received Prepare message, blk = %lu, from = %d, rand = %lu, my current state = %d, max_member_idx=%d\n", term->my_idx, msg->blockNum, msg->sender_idx, msg->rand, instance->state, instance->max_member_idx);

    int socket = term->sock;
    pthread_mutex_lock(&instance->state_lock);
    if (instance->state == STATE_EMPTY || instance->state == STATE_PREPARED || instance->state == STATE_PREPARE_SENT) {
        if (msg->rand > instance->max_rand) {

            memcpy(instance->addr, msg->addr, 20);
            instance->max_rand = msg->rand;
            instance->max_member_idx = msg->sender_idx;
            //Prepared for a proposal with higher ballot.
            //If the current node is electing for this instance,
            //it should have failed.
            //Notify the electing thread with the news.

            instance->state = STATE_PREPARED;
            pthread_cond_broadcast(&instance->cond);



            Message resp;
            resp.term_start = term->start_block;
            resp.rand = msg->rand;
            resp.blockNum = msg->blockNum;
            resp.message_type = ELEC_PREPARED;
            resp.sender_idx = term->my_idx;
            memcpy(resp.addr, term->my_account, 20);
            char *output = serialize(&resp);
            debug_print("[%d] Sending PrepareED to [%d], blk = %lu, rand = %lu\n", term->my_idx, msg->sender_idx, msg->blockNum,  msg->rand);
            if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *)si_other, si_len) == -1) {
                perror("Failed to send resp");
            }
            free(output);
        }else{
            debug_print("[%d] Not sending PrepareD, blk = %ld, from = %d, rand = %lu, my current state = %d, my current max_rand= %lu\n", term->my_idx, msg->blockNum, msg->sender_idx, msg->rand, instance->state, instance->max_rand);
        }
    }
    if (instance->state == STATE_PREPARED && msg->sender_idx == instance->max_member_idx && msg->sender_idx != term->my_idx){
        //resend    
        Message resp;
        resp.term_start = term->start_block;
        resp.rand = msg->rand;
        resp.blockNum = msg->blockNum;
        resp.message_type = ELEC_PREPARED;
        resp.sender_idx = term->my_idx;
        memcpy(resp.addr, term->my_account, 20);
        char *output = serialize(&resp);
        debug_print("[%d] reSending PrepareED to [%d], blknum = %lu,  rand = %lu\n", term->my_idx, msg->sender_idx,msg->blockNum,  msg->rand);
        if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *)si_other, si_len) == -1) {
            perror("Failed to send resp");
        }
        free(output);
    }


    if (instance->state == STATE_ELECTED){
        Message resp;
        resp.term_start = term->start_block;
        resp.rand = instance->max_rand;
        resp.message_type = ELEC_NOTIFY;
        resp.sender_idx = term->my_idx;
        resp.blockNum = msg->blockNum;
        memcpy(resp.addr, term->my_account, 20);
        char *output = serialize(&resp);
        debug_print("[%d] Sending Notify to [%d], rand = %lu\n",term->my_idx,  msg->sender_idx, msg->rand);
        if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *)si_other, si_len) == -1) {
            perror("Failed to send resp");
        }
        free(output);
    }
    if (instance->state == STATE_CONFIRM_SENT || instance->state == STATE_TRANFERRED){
        if (msg->rand > instance->max_rand){
            debug_print("[%d] transfering my current vote %d , blk = %lu, count = %d\n", term->my_idx, msg->sender_idx, msg->blockNum, instance->confirmed_addr_count);
            instance->state = STATE_TRANFERRED;
            instance->max_rand = msg->rand;
            instance->max_member_idx = msg->sender_idx;
            pthread_cond_broadcast(&instance->cond);
            int i;
            Message resp;
            resp.term_start = term->start_block;
            resp.rand = instance->max_rand;
            resp.message_type = ELEC_PREPARED;
            resp.sender_idx = term->my_idx;
            resp.blockNum = msg->blockNum;
            
            for (i = 0; i<instance->confirmed_addr_count; i++){
                debug_print("[%d] Transferring my vote[%d] to [%d], total vote = %d\n", term->my_idx, i, msg->sender_idx, instance->confirmed_addr_count);
                memcpy(resp.addr, instance->confirmed_addr[i], 20);
                char *output = serialize(&resp);
                if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *)si_other, si_len) == -1) {
                    perror("Failed to send resp");
                }
                free(output);
            }
        }
    }


    if (instance->state == STATE_TRANFERRED && msg->sender_idx == instance->max_member_idx){
        debug_print("[%d] Received resending prepared from %d, resending my confirm to get all ConfirmED, blk=%lu\n",term->my_idx, msg->sender_idx, msg->blockNum);
        Message resp;
        resp.term_start = term->start_block; 
        resp.rand = 0;
        resp.message_type = ELEC_CONFIRM;
        resp.sender_idx = term->my_idx;
        resp.blockNum = msg->blockNum;
        memcpy(resp.addr, term->my_account, 20);
        int ret = broadcast(&resp, term);
        if (ret != 0) {
            perror("failed to broadcast Confirm message");
        }
        
        debug_print("[%d] Resending my current vote %d , blk = %lu, count = %d\n", term->my_idx, msg->sender_idx, msg->blockNum, instance->confirmed_addr_count);
        int i;
        resp.term_start = term->start_block; 
        resp.rand = instance->max_rand;
        resp.message_type = ELEC_PREPARED;
        resp.sender_idx = term->my_idx;
        resp.blockNum = msg->blockNum;

    
        
        for (i = 0; i<instance->confirmed_addr_count; i++){
            debug_print("[%d] ReTransferring my vote[%d] to [%d], total vote = %d\n", term->my_idx, i, msg->sender_idx, instance->confirmed_addr_count);
            memcpy(resp.addr, instance->confirmed_addr[i], 20);
            char *output = serialize(&resp);
            if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *)si_other, si_len) == -1) {
                perror("Failed to send resp");
            }
            free(output);
        }
    }

    pthread_mutex_unlock(&instance->state_lock);
}


void handle_prepared(const Message *msg, const struct sockaddr_in *si_other, Term_t *term) {
    uint64_t offset = msg->blockNum - term->start_block;
    if (offset >= term->len){
        debug_print("Message NOT for my term, start=%lu, len = %lu, blk_num = %lu\n", term->start_block, term->len,  msg->blockNum);
        return;
    }
    instance_t *instance = &term->instances[offset];
    int socket = term->sock;
    debug_print("[%d] Received PrepareD message from %d, current state = %d, blk = %ld\n", term->my_idx, msg->sender_idx, instance->state, msg->blockNum);

    pthread_mutex_lock(&instance->state_lock);
    
    if (instance->state == STATE_PREPARE_SENT) {
        int ret = insert_addr(instance->prepared_addr, msg->addr, &instance->prepared_addr_count);
        debug_print("[%d] prepared count = %d\n", term->my_idx, instance->prepared_addr_count);
        if (ret != 0) {
            pthread_mutex_unlock(&instance->state_lock);
            return;
        }
        if (instance->prepared_addr_count > term->member_count / 2) {
            memcpy(instance->confirmed_addr[0], term->my_account, 20);
            instance->confirmed_addr_count = 1;
            Message resp;
            resp.term_start = term->start_block; 
            memcpy(resp.addr, term->my_account, 20);
            resp.message_type = ELEC_CONFIRM;
            resp.blockNum = msg->blockNum;
            resp.rand = msg->rand;
            resp.sender_idx = term->my_idx;
            ret = broadcast(&resp, term);
            if (ret != 0) {
                perror("failed to broadcast Confirm message");
            }
            //The node is still potentially ``in-control'', No need to notify.
            instance->state = STATE_CONFIRM_SENT;
        }
    }
    pthread_mutex_unlock(&instance->state_lock);
}


void handle_confirm(const Message *msg, const struct sockaddr_in *si_other, Term_t *term, socklen_t si_len) {
    uint64_t offset = msg->blockNum - term->start_block;
    if (offset >= term->len){
        debug_print("Message NOT for my term, start=%lu, len = %lu, blk_num = %lu\n", term->start_block, term->len,  msg->blockNum);
        return;
    }
    instance_t *instance = &term->instances[offset];
    debug_print("[%d] Received Confirm Msg from %d, blk = %lu, my current state = %d\n", term->my_idx, msg->sender_idx, msg->blockNum, instance->state);

    int socket = term->sock;
    pthread_mutex_lock(&instance->state_lock);
   
    if (instance ->state == STATE_CONFIRMED){
        if (msg->sender_idx == instance->max_member_idx && instance->max_member_idx != term->my_idx){
            //REceived resend message.
            Message resp;
            resp.term_start = term->start_block; 
            memcpy(resp.addr, term->my_account, 20);
            resp.rand = msg->rand;
            resp.blockNum = msg->blockNum;
            resp.message_type = ELEC_CONFIRMED;
            resp.sender_idx = term->my_idx; 
            //REsend my ConfirmED message
            char *output = serialize(&resp);
            debug_print("[%d] resending confirmed to %d, blknum = %lu\n", term->my_idx, msg->sender_idx, msg->blockNum);

            if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *) si_other, si_len) == -1) {
                perror("Failed to send resp");
            }
            free(output);
            // I have go other confirmed message. transfer .             
            int i;
            for (i = 0; i < instance->confirmed_addr_count; i++) {
                debug_print("[%d] Resending confirm for transferred votes to %d, blk = %lu, total vote = %d", term->my_idx, msg->sender_idx, msg->blockNum, instance->confirmed_addr_count);  
                memcpy(resp.addr, instance->confirmed_addr[i], 20);
                char *output = serialize(&resp);
                if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *) si_other, si_len) == -1) {
                    perror("Failed to send resp");
                }
                free(output);
            }
        }    
        pthread_mutex_unlock(&instance->state_lock);
        return;
    }
   
   
   
    if (instance-> max_rand > msg->rand){
        debug_print("[%d] Already prepared to larger rand, Not answering confirm, blk =%lu\n", term->my_idx, msg->blockNum);
        pthread_mutex_unlock(&instance->state_lock);
        //already prepared a higher.
        return;
    }


    if (instance->state == STATE_ELECTED) {
        debug_print("[%d] I am the leader, not answering confirm, blk =%lu\n", term->my_idx, msg->blockNum);
        pthread_mutex_unlock(&instance->state_lock);
        return;
    }




    else if (instance->state == STATE_CONFIRM_SENT){
        if (msg->rand > instance->max_rand) {
            Message resp;
            resp.term_start = term->start_block; 
            instance->max_member_idx = msg->sender_idx;
            instance->max_rand = msg->rand;
            resp.rand = msg->rand;
            resp.blockNum = msg->blockNum;
            resp.message_type = ELEC_CONFIRMED;
            resp.sender_idx = term->my_idx;
            int i;
            for (i = 0; i < instance->confirmed_addr_count; i++) {
                memcpy(resp.addr, instance->confirmed_addr[i], 20);
                char *output = serialize(&resp);
                if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *) si_other, si_len) == -1) {
                    perror("Failed to send resp");
                }
                free(output);
            }
            instance->state = STATE_CONFIRMED;
            pthread_cond_broadcast(&instance->cond);
        }
    }
    else if (instance->state == STATE_TRANFERRED) {       
        Message resp;
        resp.term_start = term->start_block; 
        resp.rand = msg->rand;
        resp.blockNum = msg->blockNum;
        resp.message_type = ELEC_CONFIRMED;
        resp.sender_idx = term->my_idx;
        int i;
        for (i = 0; i<instance->confirmed_addr_count; i++){
            memcpy(resp.addr, instance->confirmed_addr[i], 20);
            char *output = serialize(&resp);
            if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *)si_other, si_len) == -1){
                perror("Failed to send resp");
            }
            free(output);
        }
        instance->state = STATE_CONFIRMED;
        pthread_cond_broadcast(&instance->cond);
    }
    else {
        //Answering the confirm message
        Message resp;
        resp.term_start = term->start_block; 
        memcpy(resp.addr, term->my_account, 20);
        resp.rand = msg->rand;
        resp.blockNum = msg->blockNum;
        resp.message_type = ELEC_CONFIRMED;
        resp.sender_idx = term->my_idx; 
        char *output = serialize(&resp);
        debug_print("[%d] Sending confirmed to %d, blknum = %lu\n", term->my_idx, msg->sender_idx, msg->blockNum);

        if (sendto(socket, output, MSG_LEN, 0, (struct sockaddr *) si_other, si_len) == -1) {
            perror("Failed to send resp");
        }
        free(output);
        instance->state = STATE_CONFIRMED;
        pthread_cond_broadcast(&instance->cond);
    }
    pthread_mutex_unlock(&instance->state_lock);
    return;
}

void handle_confirmed(const Message *msg, const struct sockaddr_in *si_other, Term_t *term) {
    debug_print("[%d] Received ConfirmED Msg from %d, blk = %lu\n",term->my_idx, msg->sender_idx, msg->blockNum);
    uint64_t offset = msg->blockNum - term->start_block;
    if (offset >= term->len){
        debug_print("Message NOT for my term, start=%lu, len = %lu, blk_num = %lu\n", term->start_block, term->len,  msg->blockNum);
        return;
    }
    instance_t *instance = &term->instances[offset];
    int socket = term->sock;
    pthread_mutex_lock(&instance->state_lock);
    if (instance->state == STATE_CONFIRM_SENT) {
        int ret = insert_addr(instance->confirmed_addr, msg->addr, &instance->confirmed_addr_count);
        if (ret != 0) {
            pthread_mutex_unlock(&instance->state_lock);
            return;
        }
        if (instance->confirmed_addr_count > term->member_count / 2) {
            Message resp;
            resp.term_start = term->start_block; 
            memcpy(resp.addr, term->my_account, 20);
            resp.message_type = ELEC_ANNOUNCE;
            resp.blockNum =  msg->blockNum;
            resp.rand = msg->rand;
            resp.sender_idx = term->my_idx;
            ret = broadcast(&resp, term);
            if (ret != 0){
                perror("failed to broadcast Confirm message");
            }
            instance->state = STATE_ELECTED;
            pthread_cond_broadcast(&instance->cond);

            }
    }
    if (instance->state == STATE_TRANFERRED || instance->state == STATE_CONFIRMED){
        debug_print("[%d] Already transfer my vote to %d, giving him a prepared msg, blk =%lu\n", term->my_idx, instance->max_member_idx, msg->blockNum); 
        //They also confirm to me, if I received a confirm message, I need to send confirmed msg on their behalves. 
        int ret = insert_addr(instance->confirmed_addr, msg->addr, &instance->confirmed_addr_count);


        Message transfer; 
        transfer.term_start = term->start_block; 
        memcpy(transfer.addr, msg->addr, 20);
        transfer.message_type = ELEC_PREPARED;
        transfer.blockNum = msg->blockNum;
        transfer.rand = msg->rand; 
        transfer.sender_idx = term->my_idx; 

        char *buf = serialize(&transfer);

        ret = sendto(socket, buf, MSG_LEN, 0, (struct sockaddr*)&term->members[instance->max_member_idx], sizeof(struct sockaddr_in));
        free(buf);
        if (ret == -1){
                perror("Failed to send message\n");
        }
    }


    pthread_mutex_unlock(&instance->state_lock);
}

void handle_notify(const Message *msg, const struct sockaddr_in *si_other, Term_t *term){
    debug_print("[%d] Received Notify Msg, blk = %lu\n", term->my_idx, msg->blockNum);
    uint64_t offset = msg->blockNum - term->start_block;
    if (offset >= term->len){
        debug_print("Message NOT for my term, start=%lu, len = %lu, blk_num = %lu\n", term->start_block, term->len,  msg->blockNum);
        return;
    }
    instance_t *instance = &term->instances[offset];
    pthread_mutex_lock(&instance->state_lock);
    instance->max_rand = msg->rand;
    instance->max_member_idx = msg->sender_idx;

    instance->state = STATE_CONFIRMED;

    pthread_cond_broadcast(&instance->cond);
    pthread_mutex_unlock(&instance->state_lock);
}

void handle_announce(const Message *msg, const struct sockaddr_in *si_other, Term_t *term){
    fprintf(stderr, "[Leader] %lu %d\n", msg->blockNum, msg->sender_idx);
    uint64_t offset = msg->blockNum - term->start_block;
    if (offset >= term->len){
        debug_print("Message NOT for my term, start=%lu, len = %lu, blk_num = %lu\n", term->start_block, term->len,  msg->blockNum);
        return;
    }
    instance_t *instance = &term->instances[offset];
    pthread_mutex_lock(&instance->state_lock);
    instance->max_rand = msg->rand;
    instance->max_member_idx = msg->sender_idx;
    if (instance->state != STATE_ELECTED) {
        instance->state = STATE_CONFIRMED;
    }
    pthread_cond_broadcast(&instance->cond);
    pthread_mutex_unlock(&instance->state_lock);
}



static void *RecvFunc(void *opaque){

    Term_t *term = (Term_t *)opaque;
    int s = term->sock;


    /*
     * debug
     */
    struct sockaddr_in foo;
    int len = sizeof(struct sockaddr_in);
    getsockname(s,  (struct sockaddr *) &foo, &len);
    fprintf(stderr, "Thread Receving network packets, listening on %s:%d, start_blk = %lu, len = %lu\n",inet_ntoa(foo.sin_addr),
            ntohs(foo.sin_port), term->start_block, term->len);



    char buffer[1024];
    int recv_len;
    struct sockaddr_in si_other;
    socklen_t si_len = sizeof(si_other);
    while(1){
        pthread_mutex_lock(&term->flag_lock);
        if (term->should_stop){
            pthread_mutex_unlock(&term->flag_lock);

            pthread_exit(NULL);
        }
        pthread_mutex_unlock(&term->flag_lock);

        recv_len = recvfrom(s, buffer, BUFLEN, 0, (struct sockaddr *)&si_other, &si_len);
        if (recv_len != MSG_LEN){
            if (errno == EAGAIN || errno == EWOULDBLOCK){
                continue;
            }
            fprintf(stderr, "Wrong Message Format\n");
        }
        Message msg = deserialize(buffer);
        if (msg.term_start != term->start_block){
            debug_print("[%d] received msg not for my term, msg->term_start = %lu, my_term start =%lu", term->my_idx, msg.term_start, term->start_block);
            continue;
        }


        switch(msg.message_type){
            case ELEC_PREPARE :
                handle_prepare(&msg, &si_other, term, si_len);
                break;
            case ELEC_PREPARED :
                handle_prepared(&msg, &si_other, term);
                break;
            case ELEC_CONFIRM :
                handle_confirm(&msg, &si_other, term, si_len);
                break;
            case ELEC_CONFIRMED :
                handle_confirmed(&msg, &si_other, term);
                break;
            case ELEC_ANNOUNCE :
                handle_announce(&msg, &si_other, term);
                break;
            case ELEC_NOTIFY:
                handle_notify(&msg, &si_other, term);
                break;

        }

    }

}
/*
return value: 
1. Elected
0. Failed
-1. Try again. 

*/


int elect(Term_t *term, uint64_t blk, uint64_t *value, int timeoutMs){
    uint64_t offset = blk - term->start_block;
    if (offset >= term->len){
        info_print("Electing NOT for my term, start=%lu, len = %lu, blk_num = %lu\n", term->start_block, term->len,  blk);
        return -1;
    }



    instance_t *instance = &term->instances[offset];

    //TODO: This is problematic. 
    // if (instance->state == STATE_PREPARE_SENT || instance->state == STATE_CONFIRM_SENT){
    //     //re-entering this instance. 
        
    //     info_print("[Election] for block %lu failed\n", blk);
    //     return -1;
    // }

    uint64_t r;
    if (instance->try_count== 0){
        //first try
        r = (uint64_t)rand();
        instance->my_rand = r; 
    }else{
        r = instance->my_rand;
    }
    pthread_mutex_lock(&instance->state_lock);

    if (instance->state == STATE_CONFIRMED){
        info_print("[Election] for block %lu failed, state = %d\n", blk, instance->state);
        pthread_mutex_unlock(&instance->state_lock);
        return 0;
    }

    if (instance->state == STATE_ELECTED){
        info_print("[Election] for block %lu failed, state = %d\n", blk, instance->state);
        *value = instance->my_rand;
        pthread_mutex_unlock(&instance->state_lock);
        return 1;
    }


    if ((r > instance->max_rand && instance->try_count== 0) || (instance->try_count>0 && instance->state == STATE_PREPARE_SENT) || (instance->try_count>0 && instance->state == STATE_CONFIRM_SENT))
    {

        instance->max_rand = r;
        memcpy(instance->prepared_addr[0], term->my_account, 20);

        Message msg;
        msg.blockNum = blk;
        msg.term_start = term->start_block; 
        
        
        if (instance->try_count== 0){
            instance->prepared_addr_count = 1;
            msg.message_type = ELEC_PREPARE;
            instance->state =  STATE_PREPARE_SENT;
            debug_print("[%d] Electing for blk %lu\n", term->my_idx, blk);
        }    
        else if  (instance->state == STATE_PREPARE_SENT){
            msg.message_type = ELEC_PREPARE;
            instance->state =  STATE_PREPARE_SENT;    
            debug_print("[%d] Retry Prepare, Current state = %d, Retry [%d] electing for block %lu; Prepared count %d, Confirmed Count %d\n", term->my_idx, instance->state, instance->try_count, blk, instance->prepared_addr_count, instance->confirmed_addr_count);
        }
        else{
            msg.message_type = ELEC_CONFIRM;
            instance->state =  STATE_CONFIRM_SENT;  
            debug_print("[%d] Retry Confirm, Current state = %d, Retry [%d] electing for block %lu; Prepared count %d, Confirmed Count %d\n", term->my_idx, instance->state, instance->try_count, blk, instance->prepared_addr_count, instance->confirmed_addr_count);
        }
        
        msg.rand = r;
        msg.sender_idx = term->my_idx;
        memcpy(msg.addr, term->my_account, 20);
            

        //Sending out prepare message
        broadcast(&msg, term);
        instance->try_count++;

        struct timespec timeToWait;
        struct timeval now;
        int rt;
        gettimeofday(&now,NULL);
        timeToWait.tv_sec = now.tv_sec+0;
        timeToWait.tv_nsec = (now.tv_usec+1000UL*timeoutMs)*1000UL;
        if (timeToWait.tv_nsec > 1000000000UL){
            timeToWait.tv_nsec -= 1000000000UL; 
            timeToWait.tv_sec += 1; 
        }
            
        rt = pthread_cond_timedwait(&instance->cond, &instance->state_lock, &timeToWait);
        int state  = instance->state;
        pthread_mutex_unlock(&instance->state_lock);
        if (state == STATE_ELECTED) {
            *value = instance->my_rand;
            info_print("[Election] as leader for block %lu\n", blk);
            return 1;
        }
        else if (state == STATE_PREPARE_SENT || state == STATE_CONFIRM_SENT){
            return -1; 
        }
        else{
            info_print("[Election] for block %lu failed\n", blk);
            return 0;
        }
        //else failed. 
    }
    else {
        pthread_mutex_unlock(&instance->state_lock);
        info_print("[Election] for block %lu failed\n", blk);
        return 0;
    }
}
int killGroup(Term_t *term) {
    pthread_mutex_lock(&term->flag_lock);
    term->should_stop = 1; 
    pthread_mutex_unlock(&term->flag_lock);
    pthread_join(term->recvt, NULL);
    //TODO: Free the term gracefully. 
    close(term->sock);
    free(term->members);
    uint64_t i; 
    for (i = 0; i<term->len; i++){
        debug_print("[%d] Freeing Instance %lu\n",term->my_idx, i);
        free_instance(term, &term->instances[i]);
    }
    free(term->instances);

    free(term); 
    return 1;
}


/*
helper functions.
*/

static char* serialize(const Message *msg){
    char *output = malloc(MSG_LEN * sizeof(char));
    memcpy(&output[0], &msg->rand, 8);
    memcpy(&output[8], &msg->blockNum, 8);
    memcpy(&output[16], &msg->message_type, 1);
    memcpy(&output[17], &msg->sender_idx, 4);
    memcpy(&output[21], msg->addr, 20);
    memcpy(&output[41], &msg->term_start, 8);
    return output;
}

static Message deserialize(char *input){
    Message msg;
    memcpy(&msg.rand, &input[0], 8);
    memcpy(&msg.blockNum, &input[8], 8);
    memcpy(&msg.message_type, &input[16], 1);
    memcpy(&msg.sender_idx, &input[17], 4);
    memcpy(msg.addr, &input[21], 20);
    memcpy(&msg.term_start, &input[41], 8);
    return msg;
}

static int insert_addr(char **addr_array, const char *addr,  int *count){
    int i;
    for (i = 0; i<*count; i++){
        if (memcmp(addr_array[i], addr, 20) == 0){
            return 1; //already in the list.
        }
    }
    memcpy(addr_array[*count], addr, 20);
    *count = *count +1;
    return 0;
}

static int broadcast(const Message *msg, Term_t *term){
    int socket = term->sock;
    int i;
    ssize_t ret;
    char *buf = serialize(msg);
    for (i = 0; i<term->member_count; i++){
        ret = sendto(socket, buf, MSG_LEN, 0, (struct sockaddr*)&term->members[i], sizeof(struct sockaddr_in));
        if (ret == -1){
            free(buf);
            perror("Failed to broadcast message\n");
            return -1;
        }
    }
    free(buf);
    return 0;
}

static void init_instance(Term_t *term, instance_t *instance){
    pthread_cond_init(&instance->cond, NULL);
    pthread_mutex_init(&instance->state_lock, NULL);

    instance->max_rand = 0;
    instance->state = STATE_EMPTY;
    instance->confirmed_addr_count = 0;
    instance->prepared_addr_count = 0;
    instance->try_count = 0;


    instance->prepared_addr=(char**)malloc(term->member_count * sizeof(char*));
    instance->confirmed_addr=(char**)malloc(term->member_count * sizeof(char*));
    int j = 0;
    for (j = 0; j<term->member_count; j++){
        instance->prepared_addr[j] = (char *)malloc(20 * sizeof(char));
        instance->confirmed_addr[j] = (char *)malloc(20 * sizeof(char));
    }
}

static void free_instance(Term_t *term, instance_t *instance){
    
    int j; 
    for (j = 0; j<term->member_count; j++){
        free(instance->prepared_addr[j]);
        free(instance->confirmed_addr[j]);
    }
    free(instance->prepared_addr);
    free(instance->confirmed_addr);
}

static int send_to_member(int index, const Message* msg, Term_t *term){
    char *buf = (char *) malloc(MSG_LEN * sizeof(char));
    int ret = sendto(term->sock, buf, MSG_LEN, 0, (struct sockaddr*)&term->members[index], sizeof(struct sockaddr_in));
    if (ret != 0){
        perror("Failed to send to member");
    }
    free(buf);
    return ret;
}
